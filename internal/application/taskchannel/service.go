// Package taskchannel is the architect channel: a task's architect session
// and the sessions it delegates talk to each other through it, the architect
// asks the person for reviews through it, and a status-check loop per
// delegate wakes the architect. It also holds the rule that only the
// architect, or a person, moves a task's status.
//
// See "Contract: Harness server ↔ Agent task tools — architect channel" and
// "Contract: Harness server ↔ Web UI — architect channel API".
package taskchannel

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TaskChannel      = (*Service)(nil)
	_ ports.TaskChannelUI    = (*Service)(nil)
	_ ports.SessionDecorator = (*Service)(nil)
	_ ports.TicketDecorator  = (*Service)(nil)
)

// Tickets is what the channel needs from planning: the task, its documents,
// and moving its status.
type Tickets interface {
	GetTicket(ctx context.Context, id string) (*domain.Ticket, error)
	PatchTicket(ctx context.Context, id string, patch ports.TicketPatch) (*domain.Ticket, error)
	ListTicketDocuments(ticketID string) []*domain.Document
}

// Repositories are the channel's own tables.
type Repositories struct {
	Messages ports.TaskMessageRepository
	Reviews  ports.ReviewRequestRepository
	Checks   ports.StatusCheckRepository
}

// Service implements the architect channel.
type Service struct {
	sessions ports.SessionRepository
	tickets  Tickets
	messages ports.TaskMessageRepository
	reviews  ports.ReviewRequestRepository
	checks   ports.StatusCheckRepository

	// Delivery puts turns into running sessions. The orchestration implements
	// it and is built after this service, so it is set during wiring. Nil
	// stores everything and delivers nothing.
	Delivery ports.TurnDelivery
	// Announcer speaks on session streams; Feed on project feeds. Nil is
	// silent.
	Announcer ports.SessionAnnouncer
	Feed      ports.ProjectChangeSink
	// Artifacts checks that an artifact a message names is on the task. Nil
	// refuses every artifact id.
	Artifacts interface {
		GetArtifact(ctx context.Context, id string) (*domain.Artifact, error)
	}
	// Agents names the agent a session runs, in turn headers. Nil uses the
	// agent id.
	Agents interface {
		GetAgent(ctx context.Context, id string) (*domain.Agent, error)
	}

	// clock is the time source; SetClock swaps it while the scheduler runs.
	clock atomic.Pointer[func() time.Time]
	// mu serialises read-modify-write of status checks: the scheduler, tool
	// calls, lifecycle events and delivery callbacks all change them.
	mu   sync.Mutex
	kick chan struct{}
}

// New returns the channel. Set Delivery, Announcer, Feed, Artifacts and
// Agents before serving.
func New(sessions ports.SessionRepository, tickets Tickets, repos Repositories) *Service {
	s := &Service{
		sessions: sessions, tickets: tickets,
		messages: repos.Messages, reviews: repos.Reviews, checks: repos.Checks,
		kick: make(chan struct{}, 1),
	}
	s.SetClock(time.Now)
	return s
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.clock.Store(&now) }

func (s *Service) now() time.Time { return (*s.clock.Load())() }

func notFound(code, msg string) error { return &domain.StructuredError{Code: code, Message: msg} }
func invalid(msg string) error        { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

var (
	errNoArchitect   = &domain.StructuredError{Code: "NO_ARCHITECT", Message: "this task has no architect session"}
	errArchitectOnly = &domain.StructuredError{Code: "ARCHITECT_ONLY", Message: "only the task's architect session can do this"}
)

// scope is a session, its task, and every session on that task.
type scope struct {
	session *domain.Session
	ticket  *domain.Ticket
	all     []*domain.Session
}

func (sc *scope) architect() *domain.Session { return domain.TaskArchitect(sc.all) }

// member is the session on this task with id, or nil.
func (sc *scope) member(id string) *domain.Session {
	i := slices.IndexFunc(sc.all, func(s *domain.Session) bool { return s.ID == id })
	if i < 0 {
		return nil
	}
	return sc.all[i]
}

func (s *Service) scopeOf(ctx context.Context, sessionID string) (*scope, error) {
	sess := s.sessions.Get(sessionID)
	if sess == nil {
		return nil, notFound("SESSION_NOT_FOUND", "session not found")
	}
	if sess.TicketID == "" {
		return nil, notFound("SESSION_NOT_ON_TASK", "this session is not working on a task")
	}
	tk, err := s.tickets.GetTicket(ctx, sess.TicketID)
	if err != nil {
		return nil, err
	}
	return &scope{session: sess, ticket: tk, all: s.taskSessions(sess.TicketID)}, nil
}

func (s *Service) taskSessions(ticketID string) []*domain.Session {
	return s.sessions.List(ports.SessionFilter{TicketID: ticketID})
}

// architectScope is the caller's scope when the caller is its task's
// architect; ARCHITECT_ONLY otherwise.
func (s *Service) architectScope(ctx context.Context, architectID string) (*scope, error) {
	sc, err := s.scopeOf(ctx, architectID)
	if err != nil {
		return nil, err
	}
	if a := sc.architect(); a == nil || a.ID != architectID {
		return nil, errArchitectOnly
	}
	return sc, nil
}

// TaskArchitect is the task's architect session, or nil.
func (s *Service) TaskArchitect(_ context.Context, taskID string) (*domain.Session, error) {
	return domain.TaskArchitect(s.taskSessions(taskID)), nil
}

// SessionRole is what the session is on its task.
func (s *Service) SessionRole(_ context.Context, sessionID string) (domain.SessionRole, error) {
	sess := s.sessions.Get(sessionID)
	if sess == nil {
		return "", notFound("SESSION_NOT_FOUND", "session not found")
	}
	if sess.TicketID == "" {
		return domain.RolePeer, nil
	}
	role, _ := domain.RoleOf(sessionID, s.taskSessions(sess.TicketID))
	return role, nil
}

// SetTaskStatus moves the session's task, under the authority rule.
func (s *Service) SetTaskStatus(ctx context.Context, bySessionID string, status domain.TicketStatus, reason string) (*domain.Ticket, error) {
	if status == "" {
		return nil, invalid("status is required: one of backlog, todo, in_progress, review, done")
	}
	if len([]rune(reason)) > domain.MaxStatusReasonLen {
		return nil, invalid("reason is too long: at most 280 characters")
	}
	sc, err := s.scopeOf(ctx, bySessionID)
	if err != nil {
		return nil, err
	}
	if a := sc.architect(); a != nil && a.ID != bySessionID {
		return nil, &domain.StructuredError{Code: "TASK_STATUS_OWNED_BY_ARCHITECT",
			Message: "the task's architect session (" + a.ID + ") owns its status: report to it with message_architect instead"}
	}
	return s.tickets.PatchTicket(ctx, sc.ticket.ID, ports.TicketPatch{Status: &status, StatusReason: reason, BySessionID: bySessionID})
}

// checkAttachments refuses documents not linked to the task and artifacts not
// published on it.
func (s *Service) checkAttachments(ctx context.Context, ticketID string, docs, artifacts []string) error {
	if len(docs) > 0 {
		linked := map[string]bool{}
		for _, d := range s.tickets.ListTicketDocuments(ticketID) {
			linked[d.ID] = true
		}
		for _, id := range docs {
			if !linked[id] {
				return invalid("document " + id + " is not linked to this task")
			}
		}
	}
	for _, id := range artifacts {
		if s.Artifacts == nil {
			return invalid("artifact " + id + " is not on this task")
		}
		a, err := s.Artifacts.GetArtifact(ctx, id)
		if err != nil || a == nil || a.TicketID != ticketID {
			return invalid("artifact " + id + " is not on this task")
		}
	}
	return nil
}

func ids(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// agentName is how turn headers name a session's agent.
func (s *Service) agentName(sess *domain.Session) string {
	if sess == nil || sess.AgentID == "" {
		return "claude"
	}
	if s.Agents != nil {
		if a, err := s.Agents.GetAgent(context.Background(), sess.AgentID); err == nil && a != nil && a.Name != "" {
			return a.Name
		}
	}
	return sess.AgentID
}

// --- announcing ---

func (s *Service) feed(c ports.ProjectChange) {
	if s.Feed != nil {
		s.Feed.AnnounceChange(c)
	}
}

// tell puts ev on each named session's stream, once per session.
func (s *Service) tell(ev ports.SessionEvent, sessionIDs ...string) {
	if s.Announcer == nil {
		return
	}
	seen := map[string]bool{}
	for _, id := range sessionIDs {
		if id == "" || seen[id] || s.sessions.Get(id) == nil {
			continue
		}
		seen[id] = true
		s.Announcer.Announce(id, ev)
	}
}

func (s *Service) emitMessage(m *domain.TaskMessage) {
	s.feed(ports.ProjectChange{TaskMessage: m})
	s.tell(ports.SessionEvent{Type: "task_message", TaskMessage: m, At: s.now()}, m.FromSessionID, m.ToSessionID)
}

func (s *Service) emitReview(r *domain.ReviewRequest) {
	s.feed(ports.ProjectChange{ReviewRequest: r})
	s.tell(ports.SessionEvent{Type: "review_request", ReviewRequest: r, At: s.now()}, r.ArchitectSessionID, r.AboutSessionID)
	// The task's pending count changed with it.
	if tk, err := s.tickets.GetTicket(context.Background(), r.TaskID); err == nil && s.Feed != nil {
		s.Feed.AnnounceChange(ports.ProjectChange{Ticket: tk})
	}
}

func (s *Service) emitCheck(c *domain.StatusCheck, firedAt *time.Time, delivered *bool) {
	ev := &ports.StatusCheckEvent{StatusCheck: *c, FiredAt: firedAt, Delivered: delivered}
	s.feed(ports.ProjectChange{StatusCheck: ev})
	s.tell(ports.SessionEvent{Type: "status_check", StatusCheck: ev, At: s.now()}, c.ArchitectSessionID, c.DelegateSessionID)
	// The delegate's record carries its loop.
	if d := s.sessions.Get(c.DelegateSessionID); d != nil && s.Feed != nil {
		s.DecorateSessions(d)
		s.Feed.AnnounceChange(ports.ProjectChange{Session: d})
	}
}

func (s *Service) emitStatus(c domain.TaskStatusChange) {
	s.feed(ports.ProjectChange{TaskStatus: &c})
	var live []string
	for _, sess := range s.taskSessions(c.TaskID) {
		if !sess.Status.IsTerminal() {
			live = append(live, sess.ID)
		}
	}
	s.tell(ports.SessionEvent{Type: "task_status", TaskStatus: &c, At: s.now()}, live...)
}

// --- decorating ---

// DecorateSessions fills each session's role, its task's architect and the
// loop checking on it.
func (s *Service) DecorateSessions(sessions ...*domain.Session) {
	byTicket := map[string][]*domain.Session{}
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		sess.Role, sess.ArchitectSessionID, sess.StatusCheck = domain.RolePeer, nil, nil
		if sess.TicketID == "" {
			continue
		}
		all, ok := byTicket[sess.TicketID]
		if !ok {
			all = s.taskSessions(sess.TicketID)
			byTicket[sess.TicketID] = all
		}
		role, arch := domain.RoleOf(sess.ID, all)
		sess.Role = role
		if arch != "" {
			sess.ArchitectSessionID = &arch
		}
		sess.StatusCheck = s.checks.Get(sess.ID)
	}
}

// DecorateTickets fills each ticket's architect and pending review count.
func (s *Service) DecorateTickets(tickets ...*domain.Ticket) {
	for _, tk := range tickets {
		if tk == nil {
			continue
		}
		tk.ArchitectSessionID = nil
		if a := domain.TaskArchitect(s.taskSessions(tk.ID)); a != nil {
			id := a.ID
			tk.ArchitectSessionID = &id
		}
		tk.PendingReviews = s.reviews.CountPending(tk.ID)
	}
}
