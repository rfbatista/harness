// Package branchgate is the harness's guard on an agent session's branch
// actions: the asking hook files a branch request per action and holds the
// command until the person answers, and the git shim asks for the branch grant
// an approval leaves (ADR 0069).
package branchgate

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

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
	// Pushes keeps the pushes a session's git ran under a grant (ADR 0071); nil
	// keeps none.
	Pushes ports.PushRecords
	// Commits counts the commits a push sends, read where git runs from the
	// remote-tracking ref (no network); known is false, and the count is
	// against base, when that ref does not exist. BaseOf is where the session's
	// branch was cut from. Either nil leaves the count out of the request.
	Commits func(dir, branch, remote, remoteBranch, base string) (n int, known bool, err error)
	BaseOf  func(sess *domain.Session) string
	// Waiter shows a session as waiting for approval; nil shows nothing.
	Waiter ports.BranchRequestWaiter
	// Turns delivers the person's answer to a session whose hook is gone, as
	// a turn; nil delivers none.
	Turns ports.TurnDelivery
	// Events and Feed announce each change of a request: the inbox follows
	// the first, the project feed the second; nil announces nothing.
	Events ports.EventPublisher
	Feed   ports.ProjectChangeSink
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
			a.Dir = dir
			if (a.Refused != "" || a.Kind.Gated()) && !switchesToSessionBranch(sess, a) {
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
	if req.Refused != "" {
		return ports.BranchGrantAnswer{Reason: req.Refused}
	}
	var changed []domain.BranchRequest
	defer func() { s.announce(ctx, sess.ProjectID, changed...) }() // after the unlock
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
		changed = append(changed, r)
		return ports.BranchGrantAnswer{Allow: true, RequestID: r.ID}
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
	for _, a := range actions {
		if a.Refused != "" {
			return ports.BranchHookVerdict{Decision: "deny", Reason: a.Refused}
		}
	}
	var filed []filedRequest
	for _, a := range actions {
		f, err := s.file(ctx, sess, a, command, in.ToolInput.Description)
		if err != nil {
			return ports.BranchHookVerdict{Decision: "deny", Reason: "The branch request could not be filed: " + err.Error()}
		}
		filed = append(filed, f)
	}
	// The command runs only when every request is approved: the first denial
	// (or expiry) ends the wait.
	answers := make(chan string, len(filed))
	for _, f := range filed {
		go func() {
			select {
			case <-f.answered:
				answers <- f.id
			case <-ctx.Done():
			}
		}()
	}
	var notes []string
	for range filed {
		var id string
		select {
		case id = <-answers:
		case <-ctx.Done():
			s.forgetWaiters(filed)
			return ports.BranchHookVerdict{Decision: "deny", Reason: "The hook stopped waiting for its branch requests"}
		}
		r, err := s.d.Requests.Get(ctx, id)
		if err != nil || r == nil {
			return ports.BranchHookVerdict{Decision: "deny", Reason: "Branch request " + id + " could not be read"}
		}
		switch r.State {
		case domain.BranchRequestApproved:
			if r.Note != "" {
				notes = append(notes, r.Note)
			}
		case domain.BranchRequestDenied:
			reason := fmt.Sprintf("The person denied branch request %s (%s)", r.ID, r.Label())
			if r.Note != "" {
				reason += ". Note: " + r.Note
			}
			return ports.BranchHookVerdict{Decision: "deny", Reason: reason}
		default: // expired: the session ended with the request unanswered
			return ports.BranchHookVerdict{Decision: "deny", Reason: fmt.Sprintf("Branch request %s (%s) was never answered: it is %s", r.ID, r.Label(), r.State)}
		}
	}
	return ports.BranchHookVerdict{Decision: "allow", Context: strings.Join(notes, "\n")}
}

// forgetWaiters drops the waiters of requests whose hook is gone, so the answer
// reaches the session as a turn.
func (s *Service) forgetWaiters(filed []filedRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range filed {
		delete(s.waiters, f.id)
	}
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
	if a.Kind == domain.BranchPush {
		r.RemoteBranch = &a.RemoteBranch
		r.Commits = s.commitsToPush(sess, a)
	}
	defer func() { s.announce(ctx, sess.ProjectID, *r) }() // after the unlock
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

// commitsToPush is how many commits the push sends, nil when it cannot be read.
func (s *Service) commitsToPush(sess *domain.Session, a domain.BranchAction) *int {
	if s.d.Commits == nil || a.Branch == "" || a.Dir == "" {
		return nil
	}
	base := ""
	if s.d.BaseOf != nil {
		base = s.d.BaseOf(sess)
	}
	n, _, err := s.d.Commits(a.Dir, a.Branch, a.Remote, a.RemoteBranch, base)
	if err != nil {
		return nil
	}
	return &n
}

// Pushed implements ports.BranchGate: it keeps the push the request's grant
// let through, as the agent's.
func (s *Service) Pushed(ctx context.Context, sessionID string, p ports.BranchPushed) error {
	if s.d.Pushes == nil {
		return nil
	}
	sess := s.d.Sessions.Get(sessionID)
	r, err := s.d.Requests.Get(ctx, p.RequestID)
	if err != nil || sess == nil || r == nil || r.SessionID != sessionID || r.Action != domain.BranchPush || r.State != domain.BranchRequestUsed || r.Remote == nil {
		return errs.New("BRANCH_REQUEST_NOT_FOUND", "no used push request "+p.RequestID)
	}
	remoteBranch := r.Branch
	if r.RemoteBranch != nil {
		remoteBranch = *r.RemoteBranch
	}
	return s.d.Pushes.RecordPush(ctx, domain.RecordedPush{
		RequestID: r.ID, SessionID: sessionID, RepositoryID: r.RepositoryID, Branch: r.Branch,
		Remote: r.Remote.Name, RemoteBranch: remoteBranch, Commit: s.d.Probe.Commit(p.CWD, "refs/heads/"+r.Branch),
		At: s.d.Now().UTC(), By: domain.PushByAgent,
	})
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
	var answered *domain.BranchRequest
	defer func() {
		if answered != nil {
			s.announce(ctx, s.projectOf(answered.SessionID), *answered) // after the unlock
		}
	}()
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
	answered = r
	ch, hookWaits := s.waiters[id]
	if hookWaits {
		close(ch)
		delete(s.waiters, id)
	}
	if s.d.Waiter != nil {
		s.d.Waiter.BranchRequestSettled(r.SessionID)
	}
	if !hookWaits && s.d.Turns != nil {
		// The hook that waited is gone (a restart): the answer is a new turn.
		go s.deliver(*r)
	}
	return r, nil
}

// deliver puts the person's answer into the session as a turn.
func (s *Service) deliver(r domain.BranchRequest) {
	if _, err := s.d.Turns.Deliver(r.SessionID, "branch-request:"+r.ID, Turn(r), nil); err != nil && errs.Code(err) != "SESSION_NOT_RUNNING" {
		slog.Warn("deliver a branch request's answer", "session", r.SessionID, "request", r.ID, "err", err)
	}
}

// Restore is what a restart leaves to rebuild: a session that is still going
// shows as waiting for approval for each request that waits on the person (the
// hook that waited is gone, so the answer will be a turn), and a request whose
// session ended while the server was down expires.
func (s *Service) Restore(ctx context.Context) error {
	pending, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{State: domain.BranchRequestPending})
	if err != nil {
		return err
	}
	ended := map[string]bool{}
	for _, r := range pending {
		sess := s.d.Sessions.Get(r.SessionID)
		if sess == nil || sess.Status.IsTerminal() {
			if !ended[r.SessionID] {
				ended[r.SessionID] = true
				if err := s.expire(ctx, r.SessionID); err != nil {
					return err
				}
			}
			continue
		}
		if s.d.Waiter != nil {
			s.d.Waiter.BranchRequestFiled(r.SessionID)
		}
	}
	return nil
}

// Subscribe registers the gate's reactions to other contexts' events: the
// requests of a session that ended expire with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionEnded) error {
		return s.expire(ctx, ev.SessionID)
	})
}

// expire marks the session's pending requests, and its approved ones whose
// grant was never used, expired.
func (s *Service) expire(ctx context.Context, sessionID string) error {
	var expired []domain.BranchRequest
	defer func() { s.announce(ctx, s.projectOf(sessionID), expired...) }() // after the unlock
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range []domain.BranchRequestState{domain.BranchRequestPending, domain.BranchRequestApproved} {
		open, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{SessionID: sessionID, State: state})
		if err != nil {
			return err
		}
		for _, r := range open {
			r.State = domain.BranchRequestExpired
			if err := s.d.Requests.Save(ctx, &r); err != nil {
				return err
			}
			expired = append(expired, r)
			if state != domain.BranchRequestPending {
				continue
			}
			if ch, ok := s.waiters[r.ID]; ok {
				close(ch)
				delete(s.waiters, r.ID)
			}
			if s.d.Waiter != nil {
				s.d.Waiter.BranchRequestSettled(sessionID)
			}
		}
	}
	return nil
}

// Turn is the turn the session gets when the person answers a request whose
// hook is gone (contract "Harness server ↔ Agent sessions — branch gate" §5).
func Turn(r domain.BranchRequest) string {
	what := r.Label()
	if r.StartPoint != nil && r.Branch != "" && (r.Action == domain.BranchCreate || r.Action == domain.BranchSwitch) {
		what += " from " + r.StartPoint.Ref
	}
	note := ""
	if r.Note != "" {
		note = " Note: " + r.Note + "."
	}
	if r.State == domain.BranchRequestDenied {
		return fmt.Sprintf("[branch request %s · denied] %s.%s", r.ID, what, note)
	}
	return fmt.Sprintf("[branch request %s · approved] %s.%s Run the command again: %s.", r.ID, what, note, r.Command)
}

// ListBranchRequests implements ports.BranchGate.
func (s *Service) ListBranchRequests(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error) {
	return s.d.Requests.List(ctx, f)
}

// projectOf is the project of a session, "" when it is gone.
func (s *Service) projectOf(sessionID string) string {
	if sess := s.d.Sessions.Get(sessionID); sess != nil {
		return sess.ProjectID
	}
	return ""
}

// announce tells the inbox and the project feed that requests changed.
func (s *Service) announce(ctx context.Context, projectID string, changed ...domain.BranchRequest) {
	if projectID == "" {
		return
	}
	for _, r := range changed {
		if s.d.Events != nil {
			if err := s.d.Events.Publish(ctx, domain.BranchRequestChanged{Request: r, ProjectID: projectID}); err != nil {
				slog.Warn("announce a branch request", "request", r.ID, "err", err)
			}
		}
		if s.d.Feed != nil {
			s.d.Feed.AnnounceChange(ports.ProjectChange{BranchRequest: &ports.BranchRequestChange{BranchRequest: &r, ProjectID: projectID}})
		}
	}
}
