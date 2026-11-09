// Package interviews keeps the interviews agents ask the person: it stores
// them, numbers each session's feed and publishes what changes ("Contract:
// harness server ↔ web UI — interviews").
package interviews

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// followerBuffer is how many events a follower may fall behind before it is
// dropped and must read the list again.
const followerBuffer = 256

// claudeName is the agent name of a session that has no agent.
const claudeName = "claude"

// Deps are what the service reads and writes.
type Deps struct {
	Repo     ports.InterviewRepository
	Sessions ports.SessionRepository
	Agents   ports.AgentRepository
	Clock    func() time.Time // nil: time.Now
	// Announce puts a session's record, with its open counts, on the project's
	// feed after they change; nil announces nothing.
	Announce ports.SessionChangeAnnouncer
	// Events carries what the inbox reacts to; nil publishes nothing.
	Events ports.EventPublisher
	// Turns validates answers and renders the agent's turn; nil refuses
	// Submit and Dismiss with UNAVAILABLE.
	Turns ports.InterviewTurns
	// Delivery is the turn courier the rendered turn goes through; nil
	// delivers nothing.
	Delivery ports.TurnDelivery
}

// Service implements ports.Interviews and ports.InterviewAsker.
type Service struct {
	Deps

	// mu holds each write and its publish together, so a session's events
	// reach its followers in seq order.
	mu        sync.Mutex
	followers map[string]map[chan domain.InterviewEvent]struct{} // by session id
}

var (
	_ ports.Interviews     = (*Service)(nil)
	_ ports.InterviewAsker = (*Service)(nil)
)

// New returns a service over deps.
func New(deps Deps) *Service {
	return &Service{Deps: deps, followers: map[string]map[chan domain.InterviewEvent]struct{}{}}
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Ask stores i, an interview with no state, time or agent name yet, as open on
// its session and publishes it on the session's feed. It refuses a session
// that is not live with INTERVIEW_UNAVAILABLE and stores nothing.
func (s *Service) Ask(_ context.Context, i domain.Interview) (*domain.Interview, error) {
	sess := s.Sessions.Get(i.SessionID)
	if sess == nil || sess.Status.IsTerminal() {
		return nil, &domain.StructuredError{Code: "INTERVIEW_UNAVAILABLE", Message: "the session is not running, so it cannot ask the person"}
	}
	i.AgentName = claudeName
	if a := s.Agents.Get(sess.AgentID); sess.AgentID != "" && a != nil && a.Name != "" {
		i.AgentName = a.Name
	}
	i.State, i.AskedAt = domain.InterviewOpen, s.now()
	i.EndedAt, i.Delivery, i.DeliveredAt, i.Answers, i.DismissNote = nil, nil, nil, nil, nil

	s.mu.Lock()
	seq, err := s.Repo.Create(&i)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewAsked, Interview: &i})
	s.mu.Unlock()
	s.signal(domain.InterviewOpened{InterviewID: i.ID, SessionID: i.SessionID, Title: i.Title, Questions: len(i.Questions)})
	s.announce(i.SessionID)
	return &i, nil
}

// Change replaces the stored interview with i's id by i and publishes
// interview_changed, carrying the whole interview. A change to a state other
// than open also puts the session's lower counts on its feeds.
func (s *Service) Change(_ context.Context, i domain.Interview) (*domain.Interview, error) {
	s.mu.Lock()
	seq, err := s.Repo.Update(&i)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewChanged, Interview: &i})
	s.mu.Unlock()
	if i.State.Closed() {
		s.signal(domain.InterviewClosed{InterviewID: i.ID, SessionID: i.SessionID})
		s.announce(i.SessionID)
	}
	return &i, nil
}

// Expire closes the session's open interviews as expired, as of now, for a
// session that ended: each is published as interview_changed and the session's
// counts, now lower, go on its feeds. Each is a compare-and-set on open: one
// the person closed first stays as they closed it. Closed interviews stay as
// they are, so expiring twice changes nothing the second time.
func (s *Service) Expire(_ context.Context, sessionID string) error {
	lists, err := s.Repo.List([]string{sessionID})
	if err != nil {
		return err
	}
	list := lists[sessionID]
	if list == nil {
		return nil
	}
	now := s.now()
	var errs []error
	for _, iv := range list.Interviews {
		expired := *iv
		expired.State, expired.EndedAt = domain.InterviewExpired, &now
		var err error
		switch {
		case iv.State == domain.InterviewOpen:
			_, err = s.leaveOpen(&expired)
		case iv.Delivery != nil && *iv.Delivery == domain.InterviewQueued:
			// Sent or dismissed, its turn never went in: the agent never read it.
			expired.Delivery, expired.DeliveredAt = nil, nil
			_, err = s.leaveQueued(&expired)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// leaveQueued writes i, a sent or dismissed interview's expired state, only
// while its turn is still queued, and says whether it won. A win is published
// as interview_changed; the session's open counts do not move.
func (s *Service) leaveQueued(i *domain.Interview) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	won, seq, err := s.Repo.ExpireQueued(i)
	if err != nil || !won {
		return false, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewChanged, Interview: i})
	return true, nil
}

// leaveOpen writes i, an open interview's next state, only while the stored
// one is still open, and says whether it won. A win is published as
// interview_changed and puts the session's lower counts on its feeds.
func (s *Service) leaveOpen(i *domain.Interview) (bool, error) {
	s.mu.Lock()
	won, seq, err := s.Repo.Close(i)
	if err != nil || !won {
		s.mu.Unlock()
		return false, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewChanged, Interview: i})
	s.mu.Unlock()
	s.signal(domain.InterviewClosed{InterviewID: i.ID, SessionID: i.SessionID})
	s.announce(i.SessionID)
	return true, nil
}

// Submit closes the interview as sent with the answers and delivers the
// agent's next turn (ports.InterviewResponder).
func (s *Service) Submit(_ context.Context, id string, answers []domain.InterviewAnswer) (*domain.Interview, error) {
	if s.Turns == nil {
		return nil, errNoTurns
	}
	return s.respond(id, domain.InterviewSent, func(iv *domain.Interview) (string, error) {
		answers = normalized(answers)
		if err := s.Turns.ValidateAnswers(*iv, answers); err != nil {
			return "", &domain.StructuredError{Code: "INTERVIEW_ANSWERS_INVALID", Message: err.Error()}
		}
		iv.Answers = answers
		return s.Turns.RenderAnswers(*iv, answers), nil
	})
}

// Dismiss closes the interview as dismissed, with the note, and delivers the
// agent's next turn (ports.InterviewResponder).
func (s *Service) Dismiss(_ context.Context, id, note string) (*domain.Interview, error) {
	if s.Turns == nil {
		return nil, errNoTurns
	}
	return s.respond(id, domain.InterviewDismissed, func(iv *domain.Interview) (string, error) {
		if note != "" {
			iv.DismissNote = &note
		}
		return s.Turns.RenderDismissed(*iv, note), nil
	})
}

var errNoTurns = &domain.StructuredError{Code: "UNAVAILABLE", Message: "interviews cannot be answered here"}

// normalized gives every answer a chosen list, never null.
func normalized(in []domain.InterviewAnswer) []domain.InterviewAnswer {
	out := make([]domain.InterviewAnswer, len(in))
	for i, a := range in {
		if a.Chosen == nil {
			a.Chosen = []int{}
		}
		out[i] = a
	}
	return out
}

// respond moves the open interview to state to (sent or dismissed): prepare
// fills in what the person said and renders the turn. Only the caller that
// wins the compare-and-set on open delivers; one that loses gets the current
// interview back.
func (s *Service) respond(id string, to domain.InterviewState, prepare func(*domain.Interview) (string, error)) (*domain.Interview, error) {
	iv, err := s.Repo.Get(id)
	if err != nil {
		return nil, err
	}
	if iv.State != domain.InterviewOpen {
		return refuse(iv, to)
	}
	if sess := s.Sessions.Get(iv.SessionID); sess == nil || sess.Status.IsTerminal() {
		return s.expire(iv, to)
	}
	text, err := prepare(iv)
	if err != nil {
		return nil, err
	}
	now := s.now()
	queued := domain.InterviewQueued
	iv.State, iv.EndedAt, iv.Delivery, iv.DeliveredAt = to, &now, &queued, nil
	won, err := s.leaveOpen(iv)
	if err != nil {
		return nil, err
	}
	if !won {
		return s.current(id, to)
	}
	if err := s.deliver(iv, text); err != nil {
		return nil, err
	}
	return s.Repo.Get(id)
}

// expire closes an open interview of a session that ended, and refuses with
// INTERVIEW_EXPIRED carrying it.
func (s *Service) expire(iv *domain.Interview, to domain.InterviewState) (*domain.Interview, error) {
	now := s.now()
	expired := *iv
	expired.State, expired.EndedAt = domain.InterviewExpired, &now
	won, err := s.leaveOpen(&expired)
	if err != nil {
		return nil, err
	}
	if !won {
		return s.current(iv.ID, to)
	}
	return nil, refusal("INTERVIEW_EXPIRED", "the session ended before the answer, so the interview expired", &expired)
}

// current re-reads the interview a caller lost the race for.
func (s *Service) current(id string, to domain.InterviewState) (*domain.Interview, error) {
	iv, err := s.Repo.Get(id)
	if err != nil {
		return nil, err
	}
	return refuse(iv, to)
}

// refuse answers for an interview that is not open: the same operation that
// already succeeded returns it, anything else is INTERVIEW_NOT_OPEN carrying it.
func refuse(iv *domain.Interview, to domain.InterviewState) (*domain.Interview, error) {
	if iv.State == to {
		return iv, nil
	}
	return nil, refusal("INTERVIEW_NOT_OPEN", "the interview is "+string(iv.State)+", not open", iv)
}

// refusal is a coded error whose details carry the current interview.
func refusal(code, message string, iv *domain.Interview) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: code, Message: message},
		Details: struct {
			Interview *domain.Interview `json:"interview"`
		}{iv},
	}
}

// deliver hands the turn to the courier; the interview reads delivered, with
// its time, once the turn went in.
//
// A courier that refuses because the session ended in the meantime leaves
// nothing to wait for: the interview expires (the agent never read it) and the
// caller is refused with INTERVIEW_EXPIRED carrying it. Any other failure is
// logged and leaves the turn queued.
func (s *Service) deliver(iv *domain.Interview, text string) error {
	if s.Delivery == nil {
		return nil
	}
	_, err := s.Delivery.Deliver(iv.SessionID, "interview:"+iv.ID, text, func(at time.Time) { s.delivered(iv.ID, at) })
	if err == nil {
		return nil
	}
	if errs.Code(err) != "SESSION_NOT_RUNNING" {
		slog.Warn("deliver an interview's turn", "interview", iv.ID, "session", iv.SessionID, "err", err)
		return nil
	}
	expired := *iv
	now := s.now()
	expired.State, expired.EndedAt, expired.Delivery, expired.DeliveredAt = domain.InterviewExpired, &now, nil, nil
	if _, err := s.leaveQueued(&expired); err != nil {
		return err
	}
	cur, err := s.Repo.Get(iv.ID)
	if err != nil {
		return err
	}
	return refusal("INTERVIEW_EXPIRED", "the session ended before the agent could read the answer, so the interview expired", cur)
}

// delivered records that the interview's turn went in at at.
func (s *Service) delivered(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	iv, err := s.Repo.Get(id)
	if err != nil || iv.Delivery == nil || *iv.Delivery == domain.InterviewDelivered {
		return
	}
	done := domain.InterviewDelivered
	iv.Delivery, iv.DeliveredAt = &done, &at
	seq, err := s.Repo.Update(iv)
	if err != nil {
		slog.Warn("record an interview's delivery", "interview", id, "err", err)
		return
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewChanged, Interview: iv})
}

// Subscribe has the service expire a session's open interviews when the
// session ends.
func (s *Service) Subscribe(bus ports.EventSubscriber) {
	ports.On(bus, func(ctx context.Context, ev domain.SessionEnded) error {
		if err := s.Expire(ctx, ev.SessionID); err != nil {
			slog.Warn("expire a session's interviews", "session", ev.SessionID, "err", err)
		}
		return nil
	})
}

// signal tells the bus of ev; the interviews do not fail for a subscriber.
func (s *Service) signal(ev domain.Event) {
	if s.Events == nil {
		return
	}
	if err := s.Events.Publish(context.Background(), ev); err != nil {
		slog.Warn("announce interview event", "event", ev.EventName(), "err", err)
	}
}

// announce puts the session's record on its project's feed.
func (s *Service) announce(sessionID string) {
	if s.Announce != nil {
		s.Announce.AnnounceSession(sessionID)
	}
}

// ListInterviews reads each session's interviews.
func (s *Service) ListInterviews(_ context.Context, sessionIDs []string) (map[string]*domain.InterviewList, error) {
	return s.Repo.List(sessionIDs)
}

// FollowInterviews follows a session's feed until ctx ends. A follower that
// falls behind is dropped: its channel closes.
func (s *Service) FollowInterviews(ctx context.Context, sessionID string) (<-chan domain.InterviewEvent, error) {
	c := make(chan domain.InterviewEvent, followerBuffer)
	s.mu.Lock()
	if s.followers[sessionID] == nil {
		s.followers[sessionID] = map[chan domain.InterviewEvent]struct{}{}
	}
	s.followers[sessionID][c] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.drop(sessionID, c)
	}()
	return c, nil
}

// drop closes follower c of the session, if it is still one. Called with mu
// held.
func (s *Service) drop(sessionID string, c chan domain.InterviewEvent) {
	set := s.followers[sessionID]
	if _, ok := set[c]; !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(s.followers, sessionID)
	}
	close(c)
}

// publish hands ev to its session's followers; one that cannot take it is
// dropped. Called with mu held.
func (s *Service) publish(ev domain.InterviewEvent) {
	for c := range s.followers[ev.Interview.SessionID] {
		select {
		case c <- ev:
		default:
			s.drop(ev.Interview.SessionID, c)
		}
	}
}
