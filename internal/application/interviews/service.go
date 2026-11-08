// Package interviews keeps the interviews agents ask the person: it stores
// them, numbers each session's feed and publishes what changes ("Contract:
// harness server ↔ web UI — interviews").
package interviews

import (
	"context"
	"sync"
	"time"

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
	defer s.mu.Unlock()
	seq, err := s.Repo.Create(&i)
	if err != nil {
		return nil, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewAsked, Interview: &i})
	return &i, nil
}

// Change replaces the stored interview with i's id by i and publishes
// interview_changed, carrying the whole interview.
func (s *Service) Change(_ context.Context, i domain.Interview) (*domain.Interview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.Repo.Update(&i)
	if err != nil {
		return nil, err
	}
	s.publish(domain.InterviewEvent{Seq: seq, Kind: domain.InterviewChanged, Interview: &i})
	return &i, nil
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
