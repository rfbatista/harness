// Package branchgate is the harness's guard on an agent session's branch
// actions: the asking hook files a branch request per action and holds the
// command until the person answers, and the git shim asks for the branch grant
// an approval leaves (ADR 0069).
package branchgate

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchGate = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Sessions interface {
		Get(id string) *domain.Session
	}
	Requests ports.BranchRequestStore
	// Probe lets the classifier read git's arguments as git does.
	Probe domain.GitProbe
	// Waiter shows a session as waiting for approval; nil shows nothing.
	Waiter ports.BranchRequestWaiter
	Now    func() time.Time
}

// Service files and answers branch requests and hands out branch grants.
type Service struct {
	d  Deps
	mu sync.Mutex
	// waiters are closed when the request they are keyed by is answered; a
	// hook waiting on a request holds one.
	waiters map[string]chan struct{}
}

// NewService returns the branch gate.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d, waiters: map[string]chan struct{}{}}
}

// gatedActions are the branch actions the command makes that need the person.
// A switch back to the session's own branch is not one.
func (s *Service) gatedActions(sess *domain.Session, invocations []domain.GitInvocation, cwd string) []domain.BranchAction {
	var out []domain.BranchAction
	for _, inv := range invocations {
		dir := inv.Dir
		if dir == "" {
			dir = cwd
		}
		for _, a := range domain.ClassifyGit(inv.Args, dir, s.d.Probe) {
			if a.Kind.Gated() && !switchesToSessionBranch(sess, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

func switchesToSessionBranch(sess *domain.Session, a domain.BranchAction) bool {
	return a.Kind == domain.BranchSwitch && a.Branch != "" && a.Branch == sess.Branch
}

// Grant implements ports.BranchGate.
func (s *Service) Grant(ctx context.Context, sessionID string, req ports.BranchGrantRequest) ports.BranchGrantAnswer {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		return ports.BranchGrantAnswer{Reason: "no such session"}
	}
	if switchesToSessionBranch(sess, req.BranchAction) {
		return ports.BranchGrantAnswer{Allow: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	approved, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{SessionID: sessionID, State: domain.BranchRequestApproved})
	if err != nil {
		return ports.BranchGrantAnswer{Reason: "the branch grants could not be read"}
	}
	for _, r := range slices.Backward(approved) { // the oldest approval first
		if !r.Matches(req.BranchAction) {
			continue
		}
		r.State = domain.BranchRequestUsed
		if err := s.d.Requests.Save(ctx, &r); err != nil {
			return ports.BranchGrantAnswer{Reason: "the branch grant could not be used"}
		}
		return ports.BranchGrantAnswer{Allow: true}
	}
	return ports.BranchGrantAnswer{}
}

// Hook implements ports.BranchGate.
func (s *Service) Hook(ctx context.Context, sessionID string, in ports.BranchHookInput) ports.BranchHookVerdict {
	command := in.ToolInput.Command
	if in.ToolName != "Bash" || !strings.Contains(command, "git") {
		return ports.BranchHookVerdict{}
	}
	invocations := domain.GitInvocations(command, in.Cwd)
	if len(invocations) == 0 {
		return ports.BranchHookVerdict{}
	}
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		// Never a branch action on the gate's own failure: the shim refuses it.
		return ports.BranchHookVerdict{}
	}
	actions := s.gatedActions(sess, invocations, in.Cwd)
	if len(actions) == 0 {
		return ports.BranchHookVerdict{}
	}
	var filed []filedRequest
	for _, a := range actions {
		f, err := s.file(ctx, sess, a, command, in.ToolInput.Description)
		if err != nil {
			return ports.BranchHookVerdict{Decision: "deny", Reason: "The branch request could not be filed: " + err.Error()}
		}
		filed = append(filed, f)
	}
	var notes []string
	for _, f := range filed {
		select {
		case <-f.answered:
		case <-ctx.Done():
			return ports.BranchHookVerdict{Decision: "deny", Reason: "The hook stopped waiting for branch request " + f.id}
		}
	}
	for _, f := range filed {
		r, err := s.d.Requests.Get(ctx, f.id)
		if err != nil || r == nil {
			return ports.BranchHookVerdict{Decision: "deny", Reason: "Branch request " + f.id + " could not be read"}
		}
		if r.State == domain.BranchRequestDenied {
			reason := fmt.Sprintf("The person denied branch request %s (%s)", r.ID, Label(*r))
			if r.Note != "" {
				reason += ". Note: " + r.Note
			}
			return ports.BranchHookVerdict{Decision: "deny", Reason: reason}
		}
		if r.Note != "" {
			notes = append(notes, r.Note)
		}
	}
	return ports.BranchHookVerdict{Decision: "allow", Context: strings.Join(notes, "\n")}
}

type filedRequest struct {
	id       string
	answered chan struct{}
}

// file stores a pending request for a, and shows the session as waiting.
func (s *Service) file(ctx context.Context, sess *domain.Session, a domain.BranchAction, command, reason string) (filedRequest, error) {
	r := &domain.BranchRequest{
		SessionID: sess.ID, RepositoryID: sess.RepositoryID, SessionBranch: sess.Branch,
		Action: a.Kind, Branch: a.Branch, StartPoint: a.Start, Command: command, Via: "git", Reason: reason,
		State: domain.BranchRequestPending, RequestedAt: s.d.Now(),
	}
	if sess.TicketID != "" {
		r.TaskID = &sess.TicketID
	}
	if a.NewName != "" {
		r.NewName = &a.NewName
	}
	if a.Remote != "" || a.URL != "" {
		r.Remote = &domain.BranchRemote{Name: a.Remote, URL: a.URL}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.d.Requests.Add(ctx, r); err != nil {
		return filedRequest{}, err
	}
	ch := make(chan struct{})
	s.waiters[r.ID] = ch
	if s.d.Waiter != nil {
		s.d.Waiter.BranchRequestFiled(sess.ID)
	}
	return filedRequest{id: r.ID, answered: ch}, nil
}

// Answer implements ports.BranchGate.
func (s *Service) Answer(ctx context.Context, id, decision, note string) (*domain.BranchRequest, error) {
	var state domain.BranchRequestState
	switch decision {
	case "approve":
		state = domain.BranchRequestApproved
	case "deny":
		state = domain.BranchRequestDenied
	default:
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: `decision must be "approve" or "deny"`}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.d.Requests.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &domain.StructuredError{Code: domain.BranchRequestNotFound, Message: "no branch request " + id}
	}
	if r.State != domain.BranchRequestPending {
		return nil, &domain.DetailedError{
			StructuredError: &domain.StructuredError{Code: domain.BranchRequestAnswered, Message: "branch request " + id + " is " + string(r.State)},
			Details:         map[string]any{"request": r},
		}
	}
	now := s.d.Now()
	r.State, r.Note, r.AnsweredAt = state, strings.TrimSpace(note), &now
	if err := s.d.Requests.Save(ctx, r); err != nil {
		return nil, err
	}
	if ch, ok := s.waiters[id]; ok {
		close(ch)
		delete(s.waiters, id)
	}
	if s.d.Waiter != nil {
		s.d.Waiter.BranchRequestSettled(r.SessionID)
	}
	return r, nil
}

// ListBranchRequests implements ports.BranchGate.
func (s *Service) ListBranchRequests(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error) {
	return s.d.Requests.List(ctx, f)
}

// Label names what a request asks, for a line the person or the agent reads:
// "create feature-x", "rename a → b", "switch commit abc123".
func Label(r domain.BranchRequest) string {
	switch {
	case r.Action == domain.BranchRename && r.NewName != nil:
		return fmt.Sprintf("rename %s → %s", r.Branch, *r.NewName)
	case r.Branch == "" && r.StartPoint != nil:
		return fmt.Sprintf("%s commit %s", r.Action, r.StartPoint.Ref)
	}
	return fmt.Sprintf("%s %s", r.Action, r.Branch)
}
