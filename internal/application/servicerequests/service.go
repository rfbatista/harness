// Package servicerequests holds the changes agents ask of a project's services
// until the person answers (ADR 0083): it files a request, lets the agent's
// tool call wait for the answer, performs the approved action as the agent's
// session through the same module the person's own actions use, and expires
// what a session leaves unanswered. It follows the branch gate's pattern.
package servicerequests

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ServiceRequests = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Services ports.ServiceManager
	Store    ports.ServiceRequestStore
	Sessions interface {
		Get(id string) *domain.Session
	}
	// Agents names the agent that starts a run; nil leaves it empty.
	Agents ports.AgentLister
	// Waiter shows a session as waiting for approval; nil shows nothing.
	Waiter ports.ServiceRequestWaiter
	// Turns delivers the result of a request whose tool call is gone (a
	// restart) to its session; nil delivers none.
	Turns ports.TurnDelivery
	// Events announces each change of a request, values included, to the inbox
	// and the services feed; nil announces nothing.
	Events ports.EventPublisher
	Now    func() time.Time
}

// Service files and answers service requests.
type Service struct {
	d  Deps
	mu sync.Mutex
	// waiters belong to the tool calls blocked on a request, by request id.
	waiters map[string]*waiter
}

// waiter is a blocked tool call: done closes once the request ended, with its
// outcome or error.
type waiter struct {
	done    chan struct{}
	outcome *ports.ServiceRequestOutcome
	err     error
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d, waiters: map[string]*waiter{}}
}

// Submit implements ports.ServiceRequests.
func (s *Service) Submit(ctx context.Context, in ports.ServiceRequestInput) (*ports.ServiceRequestOutcome, error) {
	sess := s.d.Sessions.Get(in.SessionID)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no such session"}
	}
	r := s.build(ctx, sess, in)
	w := &waiter{done: make(chan struct{})}
	if err := s.file(ctx, r, w); err != nil {
		return nil, err
	}
	select {
	case <-w.done:
		return w.outcome, w.err
	case <-ctx.Done():
		// The call is gone: the request stays pending, and its answer reaches
		// the session as a turn.
		s.mu.Lock()
		delete(s.waiters, r.ID)
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// build is the request an input asks, with its readable summary.
func (s *Service) build(ctx context.Context, sess *domain.Session, in ports.ServiceRequestInput) *domain.ServiceRequest {
	r := &domain.ServiceRequest{
		ProjectID: sess.ProjectID, ServiceID: in.ServiceID, ServiceName: in.ServiceName, Action: in.Action,
		SessionID: sess.ID, TaskID: sess.TicketID, Agent: s.agentName(ctx, sess), Reason: strings.TrimSpace(in.Reason),
		Changes: []domain.ServiceRequestChange{}, Env: []domain.ServiceRequestEnv{},
		Worktree: in.Worktree, OverridesGiven: in.EnvGiven, State: domain.ServiceRequestPending,
		RequestedAt: s.d.Now().UTC().Format(time.RFC3339),
	}
	if in.Action == domain.ServiceRequestStart || in.Action == domain.ServiceRequestRestart {
		if r.Worktree == "" {
			r.Worktree = "main"
		}
		names := make([]string, 0, len(in.Env))
		for name := range in.Env {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			r.Env = append(r.Env, domain.ServiceRequestEnv{Name: name, Value: in.Env[name], Change: "set"})
		}
	}
	r.Summary = summary(r)
	return r
}

// summary is the one readable line the person reads.
func summary(r *domain.ServiceRequest) string {
	text := string(r.Action) + " service " + r.ServiceName
	switch r.Action {
	case domain.ServiceRequestStart, domain.ServiceRequestRestart:
		if r.Worktree == "own" {
			text += " in the agent's worktree"
		}
		if r.Action == domain.ServiceRequestRestart && !r.OverridesGiven {
			text += " with the same run overrides"
		} else if n := len(r.Env); n > 0 {
			names := make([]string, n)
			for i, e := range r.Env {
				names[i] = e.Name
			}
			text += " with run overrides " + strings.Join(names, ", ")
		}
	}
	return text
}

func (s *Service) agentName(ctx context.Context, sess *domain.Session) string {
	if sess.AgentID == "" || s.d.Agents == nil {
		return ""
	}
	if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil {
		return a.Name
	}
	return ""
}

// file stores the pending request and shows its session as waiting.
func (s *Service) file(ctx context.Context, r *domain.ServiceRequest, w *waiter) error {
	s.mu.Lock()
	if err := s.d.Store.Add(ctx, r); err != nil {
		s.mu.Unlock()
		return err
	}
	s.waiters[r.ID] = w
	if s.d.Waiter != nil {
		s.d.Waiter.ServiceRequestFiled(r.SessionID)
	}
	s.mu.Unlock()
	s.announce(ctx, *r)
	return nil
}

// Answer implements ports.ServiceRequests.
func (s *Service) Answer(ctx context.Context, id, decision, note string) (*domain.ServiceRequest, error) {
	var state domain.ServiceRequestState
	switch decision {
	case "approve":
		state = domain.ServiceRequestApproved
	case "deny":
		state = domain.ServiceRequestDenied
	default:
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: `decision must be "approve" or "deny"`}
	}
	s.mu.Lock()
	r, err := s.d.Store.Get(ctx, id)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if r == nil {
		s.mu.Unlock()
		return nil, &domain.StructuredError{Code: domain.ServiceRequestNotFound, Message: "no service request " + id}
	}
	if r.State != domain.ServiceRequestPending {
		s.mu.Unlock()
		return nil, &domain.StructuredError{Code: domain.RequestNotPending, Message: "service request " + id + " is " + string(r.State)}
	}
	r.State, r.Note, r.AnsweredAt = state, strings.TrimSpace(note), s.d.Now().UTC().Format(time.RFC3339)
	if err := s.d.Store.Save(ctx, r); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	w := s.waiters[id]
	if s.d.Waiter != nil {
		s.d.Waiter.ServiceRequestSettled(r.SessionID)
	}
	s.mu.Unlock()
	s.announce(ctx, *r)

	if state == domain.ServiceRequestDenied {
		s.finish(ctx, r, w, nil, &domain.StructuredError{Code: domain.ServiceRequestDeniedCode, Message: r.DeniedMessage()})
		return r, nil
	}
	// The action runs outside the lock: a stop waits for the run to end.
	outcome, perr := s.perform(context.WithoutCancel(ctx), r)
	if perr != nil {
		r.State, r.ResultError = domain.ServiceRequestFailed, oneLine(perr.Error())
	} else {
		r.State = domain.ServiceRequestDone
	}
	if err := s.d.Store.Save(ctx, r); err != nil {
		slog.Warn("store the result of a service request", "request", r.ID, "err", err)
	}
	s.announce(ctx, *r)
	s.finish(ctx, r, w, outcome, perr)
	return r, nil
}

// finish ends a blocked tool call with the result, or, when none waits any
// more, tells the session with a turn.
func (s *Service) finish(ctx context.Context, r *domain.ServiceRequest, w *waiter, outcome *ports.ServiceRequestOutcome, err error) {
	if w != nil {
		s.mu.Lock()
		delete(s.waiters, r.ID)
		s.mu.Unlock()
		w.outcome, w.err = outcome, err
		close(w.done)
		return
	}
	if s.d.Turns == nil {
		return
	}
	if _, derr := s.d.Turns.Deliver(r.SessionID, "service-request:"+r.ID, Turn(*r), nil); derr != nil {
		slog.Debug("deliver a service request's answer", "request", r.ID, "err", derr)
	}
}

// Turn is the turn a session gets when the person answers a request whose
// tool call is gone.
func Turn(r domain.ServiceRequest) string {
	head := "[service request " + r.ID + " · " + string(r.State) + "] " + r.Summary + "."
	if r.Note != "" {
		head += " Note: " + r.Note + "."
	}
	if r.ResultError != "" {
		head += " " + r.ResultError + "."
	}
	return head
}

// perform does the approved action, as the agent's session, through the same
// module path as the person's own action.
func (s *Service) perform(ctx context.Context, r *domain.ServiceRequest) (*ports.ServiceRequestOutcome, error) {
	sess := s.d.Sessions.Get(r.SessionID)
	if sess == nil {
		return nil, errors.New("the session is gone")
	}
	switch r.Action {
	case domain.ServiceRequestStart:
		svc, already, err := s.d.Services.StartService(ctx, r.ServiceID, s.startOptions(ctx, sess, r))
		if err != nil {
			return nil, err
		}
		return &ports.ServiceRequestOutcome{Service: svc, AlreadyRunning: already}, nil
	}
	return nil, errors.New("unsupported action " + string(r.Action))
}

// startOptions are the options of the approved start or restart: who starts it,
// where, and the run overrides the request carries.
func (s *Service) startOptions(ctx context.Context, sess *domain.Session, r *domain.ServiceRequest) ports.ServiceStartOptions {
	opts := ports.ServiceStartOptions{StartedBy: domain.ServiceStartedBy{Kind: "session", SessionID: sess.ID, Agent: s.agentName(ctx, sess), TaskID: sess.TicketID}}
	if r.Worktree == "own" {
		opts.Worktree = &ports.ServiceWorktree{Dir: sess.WorkingDir, Branch: sess.Branch, SessionID: sess.ID}
	}
	if r.OverridesGiven {
		opts.Env = map[string]string{}
		for _, e := range r.Env {
			if e.Change == "set" {
				opts.Env[e.Name] = e.Value
			}
		}
	}
	return opts
}

// List implements ports.ServiceRequests.
func (s *Service) List(ctx context.Context, f domain.ServiceRequestFilter) ([]domain.ServiceRequest, error) {
	return s.d.Store.List(ctx, f)
}

// Subscribe makes the requests of a session that ended expire with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionEnded) error {
		return s.expire(ctx, ev.SessionID)
	})
}

// expire ends the pending requests of a session that ended.
func (s *Service) expire(ctx context.Context, sessionID string) error {
	open, err := s.d.Store.List(ctx, domain.ServiceRequestFilter{SessionID: sessionID, State: domain.ServiceRequestPending})
	if err != nil {
		return err
	}
	for i := range open {
		r := &open[i]
		s.mu.Lock()
		cur, err := s.d.Store.Get(ctx, r.ID)
		if err != nil || cur == nil || cur.State != domain.ServiceRequestPending {
			s.mu.Unlock()
			continue
		}
		cur.State, cur.AnsweredAt = domain.ServiceRequestExpired, s.d.Now().UTC().Format(time.RFC3339)
		if err := s.d.Store.Save(ctx, cur); err != nil {
			s.mu.Unlock()
			return err
		}
		w := s.waiters[cur.ID]
		if s.d.Waiter != nil {
			s.d.Waiter.ServiceRequestSettled(sessionID)
		}
		s.mu.Unlock()
		s.announce(ctx, *cur)
		if w != nil {
			s.finish(ctx, cur, w, nil, &domain.StructuredError{Code: domain.ServiceRequestExpiredCode, Message: "the service request expired"})
		}
	}
	return nil
}

// announce tells the inbox and the services feed that a request changed.
func (s *Service) announce(ctx context.Context, r domain.ServiceRequest) {
	if s.d.Events == nil || r.ProjectID == "" {
		return
	}
	if err := s.d.Events.Publish(ctx, domain.ServiceRequestChanged{Request: r, ProjectID: r.ProjectID}); err != nil {
		slog.Warn("announce a service request", "request", r.ID, "err", err)
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
