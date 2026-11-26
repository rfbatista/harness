// Package notifications is the person's inbox across every project: it
// records notifications from what other contexts announce on the bus
// (review requests, sessions waiting for approval, tasks moving to review,
// failed sessions), takes the ones agents post, keeps read and resolved
// state, streams every committed change, and hands each new notification to
// the delivery providers bound to it.
//
// It imports no other application context: it learns what happened from
// domain events and reads names through the narrow readers below.
//
// See "Contract: Harness server ↔ Web UI — notifications", "Contract: Harness
// server ↔ Agent sessions — notification tools" and "Contract: Notifications
// ↔ delivery providers".
package notifications

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Notifications = (*Service)(nil)
	_ ports.Notifier      = (*Service)(nil)
)

const (
	defaultLimit = 50
	maxLimit     = 200
	// followerBuffer is how far a follower may fall behind before it is
	// closed and has to reload.
	followerBuffer = 64
	// agentRateLimit is how many notifications an agent may post per rolling
	// hour: per session, or per project when it posts without one.
	agentRateLimit  = 20
	agentRateWindow = time.Hour
	// Retention is how long read or resolved notifications are kept.
	Retention = 30 * 24 * time.Hour
)

// The readers look names and owners up straight from their stores. They are
// repository-shaped on purpose: the bus is synchronous, so a handler here
// runs inside the publisher's call, and going through another context's
// service (planning decorates tickets through the taskchannel's lock) could
// take a lock the publisher already holds.

// Projects finds a project; the sqlite project repository satisfies it.
type Projects interface {
	Get(ctx context.Context, id string) *domain.Project
}

// Tickets finds a task; the sqlite ticket repository satisfies it.
type Tickets interface {
	Get(ctx context.Context, id string) *domain.Ticket
}

// Sessions finds a session; the sqlite session repository satisfies it.
type Sessions interface {
	Get(id string) *domain.Session
}

// Agents finds an agent, for the name a sessions_interrupted list shows; the
// agent catalog satisfies it.
type Agents interface {
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
}

// Repositories finds a repository; the sqlite repository store satisfies it.
type Repositories interface {
	Get(ctx context.Context, id string) *domain.Repository
}

// Services finds a service, for the repository a service request is about;
// the sqlite service store satisfies it.
type Services interface {
	GetService(id string) (*domain.Service, error)
}

// Readers are what the service reads from other contexts. Agents may be nil:
// a list then names no agent.
type Readers struct {
	Projects Projects
	Tickets  Tickets
	Sessions Sessions
	Agents   Agents
	// Repositories and Services name the repository a branch or service
	// notification is about; either may be nil, and a notification then names
	// none.
	Repositories Repositories
	Services     Services
}

// Options configure delivery and time.
type Options struct {
	// Providers are the delivery providers bound in the app; see Provider.
	Providers []Provider
	// Settings holds each provider's notifications.<name>.enabled and .level;
	// nil leaves every provider at its defaults.
	Settings ports.SettingsReader
	// Clock defaults to time.Now.
	Clock func() time.Time
	// Resumer resumes the sessions Resume all interrupted lists; nil answers
	// every resume as failed.
	Resumer ports.SessionResumer
}

// Service implements the inbox.
type Service struct {
	repo     ports.NotificationRepository
	readers  Readers
	now      func() time.Time
	delivery *dispatcher
	resumer  ports.SessionResumer
	// bootedAt is when this boot began, to the stored millisecond: a
	// commit_check_off notification of an earlier boot is ended, not extended.
	bootedAt time.Time
	// resumeMu keeps two clicks of Resume all interrupted from running at
	// once: the second waits, then retries only what is still marked.
	resumeMu sync.Mutex

	// Each write (its checks, the change and the unread count) is one write
	// transaction, numbered inside it; stream sends the changes to the
	// followers in that order, which is commit order.
	stream *sequencer

	// followMu guards followers only: following never waits on a write.
	followMu  sync.Mutex
	followers map[chan ports.NotificationChange]struct{}
}

// sequencer hands committed changes on in commit order. A write takes its
// number inside its transaction; write transactions run one at a time, so
// numbers follow commit order. A change waits here, in memory, only for the
// ones numbered before it.
type sequencer struct {
	taken atomic.Uint64

	mu      sync.Mutex
	next    uint64
	pending map[uint64]*ports.NotificationChange
}

func newSequencer() *sequencer {
	return &sequencer{next: 1, pending: map[uint64]*ports.NotificationChange{}}
}

// take numbers a write; call it inside the write's transaction.
func (q *sequencer) take() uint64 { return q.taken.Add(1) }

// done hands on the change numbered n, nil when its commit failed, and every
// change after it that was waiting for it.
func (q *sequencer) done(n uint64, c *ports.NotificationChange, send func(ports.NotificationChange)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[n] = c
	for {
		c, ok := q.pending[q.next]
		if !ok {
			return
		}
		delete(q.pending, q.next)
		q.next++
		if c != nil {
			send(*c)
		}
	}
}

// New returns the service. It fails when two providers share a name or one
// has a name its settings could not be keyed by.
func New(repo ports.NotificationRepository, readers Readers, opts Options) (*Service, error) {
	now := opts.Clock
	if now == nil {
		now = time.Now
	}
	d, err := newDispatcher(opts.Providers, opts.Settings)
	if err != nil {
		return nil, err
	}
	return &Service{
		repo: repo, readers: readers, now: now, bootedAt: now().Truncate(time.Millisecond), delivery: d, resumer: opts.Resumer, stream: newSequencer(),
		followers: map[chan ports.NotificationChange]struct{}{},
	}, nil
}

// Subscribe registers the service's handlers on bus. Every handler returns
// nil and logs what failed: recording a notification never fails the use
// case that caused it.
func (s *Service) Subscribe(bus ports.EventSubscriber) {
	s.endLegacyCommitChecks()
	ports.On(bus, func(ctx context.Context, ev domain.ReviewRequestChanged) error {
		s.onReview(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionStatusChanged) error {
		s.onSessionStatus(ev)
		s.refreshInterrupted(ev.SessionID)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionsInterrupted) error {
		s.onSessionsInterrupted(ctx, ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.BranchFindingFound) error {
		s.onBranchFinding(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.BranchRequestChanged) error {
		s.onBranchRequest(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.ServiceRequestChanged) error {
		s.onServiceRequest(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionEnded) error {
		s.resolve(sessionWaitingKey(ev.SessionID), domain.OutcomeEnded)
		if ev.Status == domain.SessionFailed {
			s.onSessionFailed(ev.SessionID)
		}
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionMemoryKilled) error {
		s.onMemoryKilled(ev.Kill)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionMemoryKillFailed) error {
		s.onMemoryKillFailed(ev.Kill)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.InterviewOpened) error {
		s.onInterviewOpened(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.InterviewClosed) error {
		s.resolve(interviewKey(ev.InterviewID), interviewOutcome(ev.State))
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionCommitCheckChanged) error {
		s.onCommitCheck(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionDeleted) error {
		s.settleDeleted(ev.SessionID)
		s.resolve(sessionWaitingKey(ev.SessionID), domain.OutcomeEnded)
		s.refreshInterrupted(ev.SessionID)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.TicketStatusChanged) error {
		s.onTaskStatus(ev.Change)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.TicketDeleted) error {
		s.logged("resolve a deleted task's notifications", func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
			changed, err := tx.ResolveTask(ev.TicketID, now, domain.OutcomeEnded)
			return changed, len(changed) > 0, err
		})
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.ProjectDeleted) error {
		s.logged("delete a deleted project's notifications", func(tx ports.NotificationRepository, _ time.Time) ([]*domain.Notification, bool, error) {
			return nil, true, tx.DeleteByProject(ev.ProjectID)
		})
		return nil
	})
}

// reviewOutcome is how a review request ended, by the state it moved to.
func reviewOutcome(state domain.ReviewState) domain.NotificationOutcome {
	switch state {
	case domain.ReviewChangesRequested:
		return domain.OutcomeChangesRequested
	case domain.ReviewWithdrawn:
		return domain.OutcomeWithdrawn
	}
	return domain.OutcomeApproved
}

// interviewOutcome is how an interview ended: sent is answered, dismissed is
// the person's choice, and an expiry is nobody's: it ended.
func interviewOutcome(state domain.InterviewState) domain.NotificationOutcome {
	switch state {
	case domain.InterviewSent:
		return domain.OutcomeAnswered
	case domain.InterviewDismissed:
		return domain.OutcomeDismissed
	}
	return domain.OutcomeEnded
}

// requestOutcome is how a branch or service request left pending: denied and
// expired as they say; every other state (approved, used, done, failed) means
// it was approved.
func requestOutcome(state string) domain.NotificationOutcome {
	switch state {
	case "denied":
		return domain.OutcomeDenied
	case "expired":
		return domain.OutcomeExpired
	}
	return domain.OutcomeApproved
}

func reviewKey(id string) string         { return "review:" + id }
func sessionWaitingKey(id string) string { return "session_waiting:" + id }
func sessionFailedKey(id string) string  { return "session_failed:" + id }
func taskReviewKey(id string) string     { return "task_review:" + id }
func interviewKey(id string) string      { return "interview:" + id }

func (s *Service) onReview(ev domain.ReviewRequestChanged) {
	if ev.State != domain.ReviewPending {
		s.resolve(reviewKey(ev.ReviewID), reviewOutcome(ev.State))
		return
	}
	n := &domain.Notification{
		ProjectID: ev.ProjectID, TaskID: ev.TaskID, SessionID: ev.AboutSessionID,
		Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction,
		Actor: s.actorOfSession(ev.ArchitectSessionID),
		Link:  domain.TaskLink(ev.ProjectID, ev.TaskID), CauseKey: reviewKey(ev.ReviewID),
	}
	reviewWhat(ev.Subject).apply(n)
	s.record(n, false)
}

func branchRequestKey(id string) string { return "branch_request:" + id }

// onBranchRequest records one action notification for a pending branch request,
// and settles it once the request is answered or gone. An answer posts nothing
// new.
func (s *Service) onBranchRequest(ev domain.BranchRequestChanged) {
	r := ev.Request
	if r.State != domain.BranchRequestPending {
		s.resolve(branchRequestKey(r.ID), requestOutcome(string(r.State)))
		return
	}
	taskID := ""
	if r.TaskID != nil {
		taskID = *r.TaskID
	}
	link := domain.TaskLink(ev.ProjectID, taskID) + "?session=" + url.QueryEscape(r.SessionID) + "#branch-request-" + url.PathEscape(r.ID)
	n := &domain.Notification{
		ProjectID: ev.ProjectID, TaskID: taskID, SessionID: r.SessionID,
		Kind: domain.NotificationBranchRequest, Level: domain.NotificationAction,
		Actor: s.actorOfSession(r.SessionID), RepositoryID: r.RepositoryID,
		Body: clip(r.Reason, domain.MaxNotificationBodyLen),
		Link: link, CauseKey: branchRequestKey(r.ID),
	}
	branchRequestWhat(r).apply(n)
	s.record(n, false)
}

func serviceRequestKey(id string) string { return "service_request:" + id }

// onServiceRequest records one action notification for a pending service
// request, and settles it once the request is answered or expired. An answer
// posts nothing new. It never holds a variable value.
func (s *Service) onServiceRequest(ev domain.ServiceRequestChanged) {
	r := ev.Request
	if r.State != domain.ServiceRequestPending {
		s.resolve(serviceRequestKey(r.ID), requestOutcome(string(r.State)))
		return
	}
	n := &domain.Notification{
		ProjectID: ev.ProjectID, TaskID: r.TaskID, SessionID: r.SessionID,
		Kind: domain.NotificationServiceRequest, Level: domain.NotificationAction,
		Actor: s.actorOfSession(r.SessionID), RepositoryID: s.serviceRepository(r),
		Body: clip(r.Reason, domain.MaxNotificationBodyLen),
		Link: "/projects/" + url.PathEscape(ev.ProjectID) + "/services#service-request-" + url.PathEscape(r.ID), CauseKey: serviceRequestKey(r.ID),
	}
	serviceRequestWhat(r).apply(n)
	s.record(n, false)
}

// onBranchFinding records one action notification for a branch or push that no
// branch request explains: it names the branch, the repository and the session,
// or the sessions whose turns overlapped when none is named (ADR 0070).
func (s *Service) onBranchFinding(ev domain.BranchFindingFound) {
	who := "No session was named."
	if len(ev.Candidates) > 0 {
		who = "Turns of " + joinNames(s.agentNames(ev.Candidates)) + " overlapped it."
	}
	link := domain.TaskLink(ev.ProjectID, ev.TaskID)
	if ev.TaskID == "" {
		link = domain.ProjectLink(ev.ProjectID)
	}
	if ev.SessionID != "" {
		who = "The session of " + s.actorOfSession(ev.SessionID).Name + " made it."
		link += "?session=" + url.QueryEscape(ev.SessionID)
	}
	n := &domain.Notification{
		ProjectID: ev.ProjectID, TaskID: ev.TaskID, SessionID: ev.SessionID,
		Kind: domain.NotificationBranchFinding, Level: domain.NotificationAction,
		Actor: systemActor(domain.ActorBranchWatch), RepositoryID: ev.RepositoryID,
		Body: clip(who, domain.MaxNotificationBodyLen),
		Link: link, CauseKey: "branch_finding:" + ev.RepositoryID + ":" + string(ev.Kind) + ":" + ev.Branch + ":" + ev.FoundAt.Format(time.RFC3339Nano),
	}
	branchFindingWhat(ev).apply(n)
	s.record(n, false)
}

func (s *Service) onSessionStatus(ev domain.SessionStatusChanged) {
	if ev.ByBranchRequest {
		return
	}
	if ev.Status != domain.SessionWaitingApproval {
		outcome := domain.OutcomeApproved
		switch {
		case ev.ApprovalDenied:
			outcome = domain.OutcomeDenied
		case ev.Status.IsTerminal():
			outcome = domain.OutcomeEnded
		}
		s.resolve(sessionWaitingKey(ev.SessionID), outcome)
		return
	}
	sess := s.readers.Sessions.Get(ev.SessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", ev.SessionID)
		return
	}
	n := &domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionWaiting, Level: domain.NotificationAction,
		Actor: s.actorOfSession(sess.ID),
		Link:  domain.TaskLink(sess.ProjectID, sess.TicketID), CauseKey: sessionWaitingKey(sess.ID),
	}
	approvalWhat(sess.LastAction).apply(n)
	s.record(n, false)
}

// onInterviewOpened records one notification per interview, which the person
// settles by answering or dismissing it. It takes the session_waiting kind:
// the session waits on the person.
func (s *Service) onInterviewOpened(ev domain.InterviewOpened) {
	sess := s.readers.Sessions.Get(ev.SessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", ev.SessionID)
		return
	}
	n := &domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionWaiting, Level: domain.NotificationAction,
		Actor: s.actorOfSession(sess.ID),
		Link:  domain.TaskLink(sess.ProjectID, sess.TicketID) + "?session=" + url.QueryEscape(sess.ID), CauseKey: interviewKey(ev.InterviewID),
	}
	interviewWhat(ev.Title, ev.Questions).apply(n)
	s.record(n, true)
}

// onSessionsInterrupted posts the one sessions_interrupted notification of a
// boot that found marked sessions, resolving the one an earlier boot left open
// (ADR 0083). A boot that found none posts nothing.
func (s *Service) onSessionsInterrupted(ctx context.Context, ev domain.SessionsInterrupted) {
	var list []domain.InterruptedSession
	var lines []string
	projectID := ""
	// Each agent is looked up once: a lookup reads the whole catalog, and a
	// boot can find hundreds of sessions run by a handful of agents.
	names := map[string]string{}
	for _, id := range ev.SessionIDs {
		sess := s.readers.Sessions.Get(id)
		if sess == nil || sess.InterruptedAt == nil {
			continue
		}
		name, ok := names[sess.AgentID]
		if !ok {
			name = s.agentName(ctx, sess.AgentID)
			names[sess.AgentID] = name
		}
		entry := domain.InterruptedSession{
			SessionID: sess.ID, ProjectID: sess.ProjectID, TaskID: sess.TicketID,
			AgentName: name, Link: sessionLink(sess),
		}
		title := "(no task)"
		if tk := s.readers.Tickets.Get(ctx, sess.TicketID); sess.TicketID != "" && tk != nil {
			title = tk.Title
		}
		if projectID == "" {
			projectID = sess.ProjectID
		}
		list = append(list, entry)
		lines = append(lines, title+" · "+entry.AgentName)
	}
	if len(list) == 0 {
		return
	}
	w, title := interruptedWhat(len(list))
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		changed, err := tx.Resolve(domain.SessionsInterruptedCauseKey, now, domain.OutcomeEnded)
		if err != nil {
			return nil, false, err
		}
		created, err := tx.Create(&domain.Notification{
			ProjectID: projectID, Source: domain.NotificationFromSystem,
			Kind: domain.NotificationSessionsInterrupted, Level: domain.NotificationAction,
			Actor: systemActor(domain.ActorRestart),
			Verb:  w.verb, Object: &domain.NotificationObject{Text: w.object},
			Title: title, Body: clip(strings.Join(lines, "\n"), domain.MaxNotificationBodyLen),
			Link: "/notifications", CauseKey: domain.SessionsInterruptedCauseKey, CreatedAt: now,
			InterruptedSessions: list,
		})
		if err != nil {
			return nil, false, err
		}
		return append(changed, created), true, nil
	})
	if err != nil {
		slog.Warn("record the sessions interrupted notification", "err", err)
		return
	}
	s.delivery.enqueue(*c.Changed[len(c.Changed)-1])
}

var _ ports.InterruptedResumer = (*Service)(nil)

// ResumeInterrupted implements ports.InterruptedResumer. It resumes the listed
// sessions that still carry the mark one at a time, each through the same
// resume as a person's click, and a failure never stops the others.
func (s *Service) ResumeInterrupted(ctx context.Context, id string) (*ports.ResumeInterruptedAnswer, error) {
	s.resumeMu.Lock()
	defer s.resumeMu.Unlock()
	n := s.repo.Get(id)
	if n == nil {
		return nil, &domain.StructuredError{Code: "NOTIFICATION_NOT_FOUND", Message: "notification " + id + " not found"}
	}
	if n.Kind != domain.NotificationSessionsInterrupted {
		return nil, invalid("notification " + id + " is not a sessions_interrupted notification")
	}
	// The click starts over: an earlier failure's reason goes, and comes back
	// only if this call fails the session again.
	list := make([]domain.InterruptedSession, len(n.InterruptedSessions))
	results := make([]ports.ResumeResult, len(list))
	for i, e := range n.InterruptedSessions {
		e.Reason = ""
		list[i] = e
		results[i] = ports.ResumeResult{SessionID: e.SessionID, Outcome: ports.ResumeSkipped}
		if !s.stillMarked(e.SessionID) {
			continue
		}
		if err := s.resumeOne(ctx, e.SessionID); err != nil {
			list[i].Reason = errMessage(err)
			results[i].Outcome, results[i].Reason = ports.ResumeFailed, list[i].Reason
			continue
		}
		results[i].Outcome = ports.ResumeResumed
	}
	marked := false
	for _, e := range list {
		marked = marked || s.stillMarked(e.SessionID)
	}
	outcome := s.interruptedOutcome(list)
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		cur, err := tx.SetInterruptedSessions(id, list)
		if err != nil || cur == nil {
			return nil, false, err
		}
		if !marked && cur.ResolvedAt == nil {
			if _, err := tx.Resolve(domain.SessionsInterruptedCauseKey, now, outcome); err != nil {
				return nil, false, err
			}
			cur = tx.Get(id)
		}
		return []*domain.Notification{cur}, true, nil
	})
	if err != nil {
		return nil, err
	}
	answer := &ports.ResumeInterruptedAnswer{Results: results}
	if c != nil {
		answer.Notification = c.Changed[0]
	}
	return answer, nil
}

// resumeOne resumes one session; a panic or a missing resumer is a failure
// of that session alone.
func (s *Service) resumeOne(ctx context.Context, sessionID string) (err error) {
	if s.resumer == nil {
		return fmt.Errorf("resuming sessions is not available on this server")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("resume panicked: %v", r)
		}
	}()
	return s.resumer.ResumeSession(ctx, sessionID)
}

func errMessage(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Message
	}
	return err.Error()
}

// refreshInterrupted sends the open sessions_interrupted notification again
// when sessionID is one it lists, its state having changed, and resolves it
// when none of its sessions still carries the mark.
func (s *Service) refreshInterrupted(sessionID string) {
	open := s.repo.OpenByCause(domain.SessionsInterruptedCauseKey)
	if open == nil || !lists(open, sessionID) {
		return
	}
	s.reconcileInterrupted(open)
}

// reconcileInterrupted sends n again, or resolves it when no listed session
// still carries the mark. Sessions are read before the write: a transaction
// holds the database's one writer connection.
func (s *Service) reconcileInterrupted(n *domain.Notification) {
	marked := false
	for _, e := range n.InterruptedSessions {
		marked = marked || s.stillMarked(e.SessionID)
	}
	outcome := s.interruptedOutcome(n.InterruptedSessions)
	s.logged("refresh the sessions interrupted notification", func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		if !marked {
			changed, err := tx.Resolve(domain.SessionsInterruptedCauseKey, now, outcome)
			return changed, len(changed) > 0, err
		}
		if cur := tx.Get(n.ID); cur != nil && cur.ResolvedAt == nil {
			return []*domain.Notification{cur}, true, nil
		}
		return nil, false, nil
	})
}

// interruptedOutcome is how a sessions_interrupted notification ends once no
// listed session is marked: resumed when at least one session is back (it
// exists and has no mark), ended when every one was deleted instead.
func (s *Service) interruptedOutcome(list []domain.InterruptedSession) domain.NotificationOutcome {
	for _, e := range list {
		if sess := s.readers.Sessions.Get(e.SessionID); sess != nil && sess.InterruptedAt == nil {
			return domain.OutcomeResumed
		}
	}
	return domain.OutcomeEnded
}

// stillMarked says the session exists and carries the interrupted mark.
func (s *Service) stillMarked(sessionID string) bool {
	sess := s.readers.Sessions.Get(sessionID)
	return sess != nil && sess.InterruptedAt != nil
}

func lists(n *domain.Notification, sessionID string) bool {
	for _, e := range n.InterruptedSessions {
		if e.SessionID == sessionID {
			return true
		}
	}
	return false
}

// agentNames are the agent names of sessions, each once, in order; a session
// that is gone is left out.
func (s *Service) agentNames(sessionIDs []string) []string {
	var names []string
	for _, id := range sessionIDs {
		if a := s.actorOfSession(id); a.Kind == domain.ActorAgent && !slices.Contains(names, a.Name) {
			names = append(names, a.Name)
		}
	}
	return names
}

// joinNames is "a", "a and b", "a, b and c"; "a session" for none.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "a session"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// serviceRepository is the repository a service request is about: its
// service's, or the one a create proposes. Empty when it has none.
func (s *Service) serviceRepository(r domain.ServiceRequest) string {
	if r.Proposed != nil && r.Proposed.RepositoryID != "" {
		return r.Proposed.RepositoryID
	}
	if s.readers.Services == nil || r.ServiceID == "" {
		return ""
	}
	if svc, err := s.readers.Services.GetService(r.ServiceID); err == nil && svc != nil {
		return svc.RepositoryID
	}
	return ""
}

func systemActor(name string) domain.NotificationActor {
	return domain.NotificationActor{Kind: domain.ActorSystem, Name: name}
}

// actorOfSession is the agent of a session with the session's mode word. A
// session that is not recorded, or has no agent, is named for what it is.
func (s *Service) actorOfSession(sessionID string) domain.NotificationActor {
	sess := s.readers.Sessions.Get(sessionID)
	if sessionID == "" || sess == nil {
		return systemActor(domain.ActorHarness)
	}
	name := s.agentName(context.Background(), sess.AgentID)
	if name == "" {
		name = "agent"
	}
	return domain.NotificationActor{Kind: domain.ActorAgent, Name: name, Mode: domain.ModeWord(sess.Mode), SessionID: sess.ID}
}

// taskReviewActor is the session that moved a task to review, or the person
// when no session did.
func (s *Service) taskReviewActor(c domain.TaskStatusChange) domain.NotificationActor {
	if c.BySessionID != "" {
		return s.actorOfSession(c.BySessionID)
	}
	return domain.NotificationActor{Kind: domain.ActorPerson, Name: domain.ActorYou}
}

// legacyActor names the actor of a notification recorded before actors were
// stored, from what it still holds.
func (s *Service) legacyActor(n *domain.Notification) domain.NotificationActor {
	switch n.Kind {
	case domain.NotificationBranchFinding:
		return systemActor(domain.ActorBranchWatch)
	case domain.NotificationSessionsInterrupted:
		return systemActor(domain.ActorRestart)
	case domain.NotificationCommitCheckOff:
		return systemActor(domain.ActorHarness)
	case domain.NotificationReviewRequested:
		// The row keeps the session the review is about, not the architect
		// that asked, and naming the wrong agent is worse than naming none.
		return systemActor(domain.ActorHarness)
	}
	return s.actorOfSession(n.SessionID)
}

// agentName is the name of agent id; empty when it has none or is unknown.
func (s *Service) agentName(ctx context.Context, id string) string {
	if s.readers.Agents == nil || id == "" {
		return ""
	}
	if a, err := s.readers.Agents.GetAgent(ctx, id); err == nil && a != nil {
		return a.Name
	}
	return ""
}

// sessionLink is the page of a session: its task's, or its project's.
func sessionLink(sess *domain.Session) string {
	return domain.TaskLink(sess.ProjectID, sess.TicketID) + "?session=" + url.QueryEscape(sess.ID)
}

func (s *Service) onSessionFailed(sessionID string) {
	sess := s.readers.Sessions.Get(sessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", sessionID)
		return
	}
	n := &domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionFailed, Level: domain.NotificationInfo,
		Actor: s.actorOfSession(sess.ID),
		Link:  domain.TaskLink(sess.ProjectID, sess.TicketID), CauseKey: sessionFailedKey(sess.ID),
	}
	failedWhat(sess.LastAction).apply(n)
	s.record(n, true)
}

// onMemoryKilled records one encerramento por memória: an info notification
// per kill, linking to the session. The command is a process name and its first
// argument, never a full command line.
func (s *Service) onMemoryKilled(k domain.MemoryKill) {
	sess := s.readers.Sessions.Get(k.SessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", k.SessionID)
		return
	}
	n := &domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionMemoryKilled, Level: domain.NotificationInfo,
		Actor: systemActor(domain.ActorHarness),
		Link:  sessionLink(sess), CauseKey: "session_memory_killed:" + sess.ID + ":" + strconv.Itoa(k.PID) + ":" + strconv.FormatInt(k.KilledAt.UnixNano(), 10),
		Body: "Used " + formatGB(k.RSSBytes) + " of the " + formatGB(k.Budget) + " memory budget (" + k.Signal + "). The session keeps running.",
	}
	memoryKilledWhat(k.Command).apply(n)
	s.record(n, true)
}

// onMemoryKillFailed records a process the vigia could not end: one info
// notification per process, linking to the session.
func (s *Service) onMemoryKillFailed(k domain.MemoryKill) {
	sess := s.readers.Sessions.Get(k.SessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", k.SessionID)
		return
	}
	n := &domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionMemoryKillFailed, Level: domain.NotificationInfo,
		Actor: systemActor(domain.ActorHarness),
		Link:  sessionLink(sess), CauseKey: "session_memory_kill_failed:" + sess.ID + ":" + strconv.Itoa(k.PID),
		Body: "It still holds " + formatGB(k.RSSBytes) + " after SIGKILL and could not be ended, so the session stays over its " + formatGB(k.Budget) + " memory budget.",
	}
	memoryKillFailedWhat(k.Command).apply(n)
	s.record(n, true)
}

func (s *Service) onTaskStatus(c domain.TaskStatusChange) {
	if c.Status != domain.TicketStatusReview {
		outcome := domain.OutcomeReopened
		if c.Status == domain.TicketStatusDone {
			outcome = domain.OutcomeDone
		}
		s.resolve(taskReviewKey(c.TaskID), outcome)
		return
	}
	projectID := c.ProjectID
	if projectID == "" {
		if tk := s.readers.Tickets.Get(context.Background(), c.TaskID); tk != nil {
			projectID = tk.ProjectID
		}
	}
	if projectID == "" {
		slog.Warn("notification for a task that is not recorded", "task", c.TaskID)
		return
	}
	taskTitle := ""
	if tk := s.readers.Tickets.Get(context.Background(), c.TaskID); tk != nil {
		taskTitle = tk.Title
	}
	n := &domain.Notification{
		ProjectID: projectID, TaskID: c.TaskID, SessionID: c.BySessionID,
		Kind: domain.NotificationTaskReview, Level: domain.NotificationAction,
		Actor: s.taskReviewActor(c),
		Body:  clip(strings.TrimSpace(c.Reason), domain.MaxNotificationBodyLen),
		Link:  domain.TaskLink(projectID, c.TaskID), CauseKey: taskReviewKey(c.TaskID),
	}
	taskReviewWhat(taskTitle).apply(n)
	s.record(n, false)
}

// record stores a system notification unless its cause already has an
// unresolved one (or, with once, any one at all).
func (s *Service) record(n *domain.Notification, once bool) {
	n.Source = domain.NotificationFromSystem
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		if once && tx.AnyByCause(n.CauseKey) || !once && tx.OpenByCause(n.CauseKey) != nil {
			return nil, false, nil
		}
		n.CreatedAt = now
		created, err := tx.Create(n)
		if err != nil {
			return nil, false, err
		}
		return []*domain.Notification{created}, true, nil
	})
	if err != nil {
		slog.Warn("record a notification", "err", err)
		return
	}
	if c != nil {
		s.delivery.enqueue(*c.Changed[0])
	}
}

// resolve settles the unresolved notifications of a cause.
func (s *Service) resolve(causeKey string, outcome domain.NotificationOutcome) {
	s.logged("resolve a notification", func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		changed, err := tx.Resolve(causeKey, now, outcome)
		return changed, len(changed) > 0, err
	})
}

// change is one write, made through tx: what it changed, and whether
// anything did.
type change func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error)

// logged writes change and logs an error as what failed.
func (s *Service) logged(what string, change change) {
	if _, err := s.write(change); err != nil {
		slog.Warn(what, "err", err)
	}
}

// write runs change and the unread count in one write transaction and, when
// change says something changed, streams what changed with the count as of
// that commit, in commit order, and answers it. It answers nil when nothing
// changed.
func (s *Service) write(change change) (*ports.NotificationChange, error) {
	var (
		seq     uint64
		changed []*domain.Notification
		count   int
		open    int
		c       *ports.NotificationChange
	)
	// A numbered write always hands its number on, so a failed commit never
	// holds back the changes after it.
	defer func() {
		if seq != 0 {
			s.stream.done(seq, c, s.broadcast)
		}
	}()
	err := s.repo.Transaction(func(tx ports.NotificationRepository) error {
		var ok bool
		var err error
		changed, ok, err = change(tx, s.now())
		if err != nil || !ok {
			return err
		}
		if count, err = tx.CountUnread(context.Background()); err != nil {
			return err
		}
		if open, err = tx.CountOpen(context.Background()); err != nil {
			return err
		}
		seq = s.stream.take()
		return nil
	})
	if err != nil || seq == 0 {
		return nil, err
	}
	c = &ports.NotificationChange{Changed: s.named(context.Background(), changed...), UnreadCount: count, OpenCount: open}
	if c.Changed == nil {
		c.Changed = []*domain.Notification{}
	}
	return c, nil
}

// broadcast sends c to every follower without waiting: one that is full has
// fallen behind and is closed, so it reloads.
func (s *Service) broadcast(c ports.NotificationChange) {
	s.followMu.Lock()
	defer s.followMu.Unlock()
	for ch := range s.followers {
		select {
		case ch <- c:
		default:
			delete(s.followers, ch)
			close(ch)
		}
	}
}

// FollowNotifications streams every committed change until ctx ends or the
// follower falls behind; then the channel closes.
func (s *Service) FollowNotifications(ctx context.Context) (<-chan ports.NotificationChange, error) {
	ch := make(chan ports.NotificationChange, followerBuffer)
	s.followMu.Lock()
	s.followers[ch] = struct{}{}
	s.followMu.Unlock()
	go func() {
		<-ctx.Done()
		s.followMu.Lock()
		defer s.followMu.Unlock()
		if _, ok := s.followers[ch]; ok {
			delete(s.followers, ch)
			close(ch)
		}
	}()
	return ch, nil
}

// ListNotifications answers a page of the inbox, newest first, with the
// global unread count.
func (s *Service) ListNotifications(ctx context.Context, q ports.NotificationQuery) (*ports.NotificationPage, error) {
	limit := q.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return nil, invalid(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
	}
	if q.Kind != "" && !knownKind(q.Kind) {
		return nil, invalid("kind is not a notification kind: " + string(q.Kind))
	}
	f := ports.NotificationFilter{ProjectID: q.ProjectID, UnreadOnly: q.UnreadOnly, Open: q.Open, Actor: q.Actor, Kind: q.Kind, Since: q.Since, Limit: limit + 1}
	if q.Before != "" {
		at, id, err := decodeCursor(q.Before)
		if err != nil {
			return nil, err
		}
		f.BeforeAt, f.BeforeID = at, id
	}
	list, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.CountUnread(ctx)
	if err != nil {
		return nil, err
	}
	open, err := s.repo.CountOpen(ctx)
	if err != nil {
		return nil, err
	}
	page := &ports.NotificationPage{UnreadCount: count, OpenCount: open}
	if len(list) > limit {
		list = list[:limit]
		page.NextBefore = encodeCursor(list[limit-1])
	}
	page.Notifications = s.named(ctx, list...)
	if page.Notifications == nil {
		page.Notifications = []*domain.Notification{}
	}
	return page, nil
}

// MarkNotificationsRead marks the given ids, or every unread notification
// (of a project), read. An unknown id fails the whole call before anything
// changes. Only the notifications this call changed are answered and
// streamed; providers are not told, since read state is the inbox's.
func (s *Service) MarkNotificationsRead(ctx context.Context, req ports.MarkNotificationsRead) (*ports.NotificationChange, error) {
	if (len(req.IDs) > 0) == req.All {
		return nil, invalid("give either ids or all, not both")
	}
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		for _, id := range req.IDs {
			if tx.Get(id) == nil {
				return nil, false, &domain.StructuredError{Code: "NOTIFICATION_NOT_FOUND", Message: "notification " + id + " not found"}
			}
		}
		var changed []*domain.Notification
		var err error
		if req.All {
			changed, err = tx.MarkAllRead(req.ProjectID, now)
		} else {
			changed, err = tx.MarkRead(req.IDs, now)
		}
		return changed, len(changed) > 0, err
	})
	if err != nil {
		return nil, err
	}
	if c == nil {
		count, err := s.repo.CountUnread(ctx)
		if err != nil {
			return nil, err
		}
		open, err := s.repo.CountOpen(ctx)
		if err != nil {
			return nil, err
		}
		c = &ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: count, OpenCount: open}
	}
	return c, nil
}

// AcknowledgeNotification implements ports.Notifications: the person
// acknowledges a branch finding, which resolves it and marks it read.
func (s *Service) AcknowledgeNotification(ctx context.Context, id string) (*domain.Notification, error) {
	var current *domain.Notification
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		current = tx.Get(id)
		switch {
		case current == nil:
			return nil, false, &domain.StructuredError{Code: "NOTIFICATION_NOT_FOUND", Message: "notification " + id + " not found"}
		case current.Kind != domain.NotificationBranchFinding:
			return nil, false, invalid("notification " + id + " is not a branch finding: only a branch finding is acknowledged")
		case current.ResolvedAt != nil:
			return nil, false, nil
		}
		if _, err := tx.Resolve(current.CauseKey, now, domain.OutcomeAcknowledged); err != nil {
			return nil, false, err
		}
		if _, err := tx.MarkRead([]string{id}, now); err != nil {
			return nil, false, err
		}
		return []*domain.Notification{tx.Get(id)}, true, nil
	})
	if err != nil {
		return nil, err
	}
	if c == nil { // already resolved: as it is
		return s.named(ctx, current)[0], nil
	}
	return c.Changed[0], nil
}

// Notify records a notification an agent posts, attributed to its session
// (if any), task (if any) and project.
func (s *Service) Notify(ctx context.Context, req ports.NotifyRequest) (*domain.Notification, error) {
	title, body := strings.TrimSpace(req.Title), strings.TrimSpace(req.Body)
	if err := domain.ValidateNotificationText(title, body); err != nil {
		return nil, err
	}
	if req.ProjectID == "" {
		return nil, invalid("project_id is required")
	}
	level := domain.NotificationInfo
	if req.Level != "" {
		l, err := domain.ParseNotificationLevel(string(req.Level))
		if err != nil {
			return nil, err
		}
		level = l
	}
	link := strings.TrimSpace(req.Link)
	if link == "" {
		link = domain.TaskLink(req.ProjectID, req.TaskID)
	} else if err := domain.ValidateHarnessLink(link); err != nil {
		return nil, err
	}
	if s.readers.Projects.Get(ctx, req.ProjectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if req.TaskID != "" {
		tk := s.readers.Tickets.Get(ctx, req.TaskID)
		if tk == nil {
			return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		if tk.ProjectID != req.ProjectID {
			return nil, &domain.StructuredError{Code: "TASK_NOT_ON_PROJECT", Message: "task " + req.TaskID + " is not in project " + req.ProjectID}
		}
	}

	// The session is read before the write: a transaction holds the
	// database's one writer connection.
	actor := s.actorOfSession(req.SessionID)
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		posted, err := tx.CountAgentSince(req.SessionID, req.ProjectID, now.Add(-agentRateWindow))
		if err != nil {
			return nil, false, err
		}
		if posted >= agentRateLimit {
			who := "this session"
			if req.SessionID == "" {
				who = "this project"
			}
			return nil, false, &domain.StructuredError{Code: "NOTIFICATION_RATE_LIMITED",
				Message: fmt.Sprintf("%s has posted %d notifications in the last hour, the most allowed; keep notify for what the person has to know or do", who, agentRateLimit)}
		}
		created, err := tx.Create(&domain.Notification{
			ProjectID: req.ProjectID, TaskID: req.TaskID, SessionID: req.SessionID,
			Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: level,
			Actor: actor,
			Title: title, Body: body, Link: link, CreatedAt: now,
		})
		if err != nil {
			return nil, false, err
		}
		return []*domain.Notification{created}, true, nil
	})
	if err != nil {
		return nil, err
	}
	created := c.Changed[0]
	s.delivery.enqueue(*created)
	n := *created
	return &n, nil
}

// Prune deletes read or resolved notifications older than Retention. Unread
// ones are kept however old.
func (s *Service) Prune(_ context.Context) (int, error) {
	return s.repo.Prune(s.now().Add(-Retention))
}

// StartDelivery starts one worker per provider. Stop them with StopDelivery.
func (s *Service) StartDelivery() { s.delivery.start() }

// StopDelivery stops taking new deliveries, lets the workers finish what is
// queued until ctx ends, then cancels what is still running.
func (s *Service) StopDelivery(ctx context.Context) {
	s.delivery.close()
	s.delivery.wait(ctx)
}

// named fills each notification's current project name and task title.
// One lookup per distinct id per call; a deleted task leaves the title out.
func (s *Service) named(ctx context.Context, list ...*domain.Notification) []*domain.Notification {
	if len(list) == 0 {
		return nil
	}
	projects, tasks, repos := map[string]string{}, map[string]string{}, map[string]string{}
	out := make([]*domain.Notification, 0, len(list))
	for _, n := range list {
		c := *n
		name, ok := projects[c.ProjectID]
		if !ok {
			if p := s.readers.Projects.Get(ctx, c.ProjectID); p != nil {
				name = p.Name
			}
			projects[c.ProjectID] = name
		}
		c.ProjectName = name
		if c.RepositoryID != "" && s.readers.Repositories != nil {
			name, ok := repos[c.RepositoryID]
			if !ok {
				if r := s.readers.Repositories.Get(ctx, c.RepositoryID); r != nil {
					name = r.DisplayName()
				}
				repos[c.RepositoryID] = name
			}
			c.RepositoryName = name
		}
		if c.Actor.Kind == "" {
			c.Actor = s.legacyActor(&c)
		}
		if c.TaskID != "" {
			title, ok := tasks[c.TaskID]
			if !ok {
				if tk := s.readers.Tickets.Get(ctx, c.TaskID); tk != nil {
					title = tk.Title
				}
				tasks[c.TaskID] = title
			}
			c.TaskTitle = title
		}
		if len(c.ListedSessions) > 0 {
			c.ListedSessions = s.listedStates(ctx, c.ListedSessions, tasks)
		}
		if len(c.InterruptedSessions) > 0 {
			c.InterruptedSessions = s.interruptedStates(ctx, c.InterruptedSessions, tasks)
		}
		out = append(out, &c)
	}
	return out
}

// interruptedStates is list with each session's state and its task's current
// title as of now. The stored list is not changed.
func (s *Service) interruptedStates(ctx context.Context, list []domain.InterruptedSession, tasks map[string]string) []domain.InterruptedSession {
	out := make([]domain.InterruptedSession, len(list))
	for i, e := range list {
		sess := s.readers.Sessions.Get(e.SessionID)
		switch {
		case sess == nil:
			e.State = domain.InterruptedSessionGone
		case sess.InterruptedAt == nil:
			e.State = domain.InterruptedSessionResumed
		case e.Reason != "":
			e.State = domain.InterruptedSessionFailed
		default:
			e.State = domain.InterruptedSessionInterrupted
		}
		if e.State != domain.InterruptedSessionFailed {
			e.Reason = ""
		}
		if e.TaskID != "" {
			title, ok := tasks[e.TaskID]
			if !ok {
				if tk := s.readers.Tickets.Get(ctx, e.TaskID); tk != nil {
					title = tk.Title
				}
				tasks[e.TaskID] = title
			}
			e.TaskTitle = title
		}
		out[i] = e
	}
	return out
}

func knownKind(k domain.NotificationKind) bool {
	for _, known := range domain.NotificationKinds {
		if k == known {
			return true
		}
	}
	return false
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

// clip cuts s to at most n characters, marking a cut with an ellipsis.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// A cursor is base64url of "<created_at unix ms>|<id>" of the last
// notification of a page.
func encodeCursor(n *domain.Notification) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(n.CreatedAt.UnixMilli(), 10) + "|" + n.ID))
}

var errBadCursor = invalid("before is not a cursor this server answered")

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", errBadCursor
	}
	ms, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return time.Time{}, "", errBadCursor
	}
	at, err := strconv.ParseInt(ms, 10, 64)
	if err != nil || at <= 0 {
		return time.Time{}, "", errBadCursor
	}
	return time.UnixMilli(at), id, nil
}
