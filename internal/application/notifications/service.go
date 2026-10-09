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
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
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
	Get(id string) *domain.Project
}

// Tickets finds a task; the sqlite ticket repository satisfies it.
type Tickets interface {
	Get(id string) *domain.Ticket
}

// Sessions finds a session; the sqlite session repository satisfies it.
type Sessions interface {
	Get(id string) *domain.Session
}

// Readers are what the service reads from other contexts.
type Readers struct {
	Projects Projects
	Tickets  Tickets
	Sessions Sessions
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
}

// Service implements the inbox.
type Service struct {
	repo     ports.NotificationRepository
	readers  Readers
	now      func() time.Time
	delivery *dispatcher

	// mu orders every write: dedup check, repository write, unread count,
	// the change sent to followers and the enqueue to providers all happen
	// under it, so followers see changes in commit order with the count as
	// of each commit. Nothing under it waits on the network.
	mu        sync.Mutex
	followers map[chan ports.NotificationChange]struct{}
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
		repo: repo, readers: readers, now: now, delivery: d,
		followers: map[chan ports.NotificationChange]struct{}{},
	}, nil
}

// Subscribe registers the service's handlers on bus. Every handler returns
// nil and logs what failed: recording a notification never fails the use
// case that caused it.
func (s *Service) Subscribe(bus ports.EventSubscriber) {
	ports.On(bus, func(ctx context.Context, ev domain.ReviewRequestChanged) error {
		s.onReview(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionStatusChanged) error {
		s.onSessionStatus(ev)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionEnded) error {
		s.resolve(sessionWaitingKey(ev.SessionID))
		if ev.Status == domain.SessionFailed {
			s.onSessionFailed(ev.SessionID)
		}
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.SessionDeleted) error {
		s.resolve(sessionWaitingKey(ev.SessionID))
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.TicketStatusChanged) error {
		s.onTaskStatus(ev.Change)
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.TicketDeleted) error {
		s.write("resolve a deleted task's notifications", func(now time.Time) ([]*domain.Notification, bool, error) {
			changed, err := s.repo.ResolveTask(ev.TicketID, now)
			return changed, len(changed) > 0, err
		})
		return nil
	})
	ports.On(bus, func(ctx context.Context, ev domain.ProjectDeleted) error {
		s.write("delete a deleted project's notifications", func(time.Time) ([]*domain.Notification, bool, error) {
			return nil, true, s.repo.DeleteByProject(ev.ProjectID)
		})
		return nil
	})
}

func reviewKey(id string) string         { return "review:" + id }
func sessionWaitingKey(id string) string { return "session_waiting:" + id }
func sessionFailedKey(id string) string  { return "session_failed:" + id }
func taskReviewKey(id string) string     { return "task_review:" + id }

func (s *Service) onReview(ev domain.ReviewRequestChanged) {
	if ev.State != domain.ReviewPending {
		s.resolve(reviewKey(ev.ReviewID))
		return
	}
	s.record(&domain.Notification{
		ProjectID: ev.ProjectID, TaskID: ev.TaskID, SessionID: ev.AboutSessionID,
		Kind: domain.NotificationReviewRequested, Level: domain.NotificationAction,
		Title: clip("Review requested: "+strings.TrimSpace(ev.Subject), domain.MaxNotificationTitleLen),
		Link:  domain.TaskLink(ev.ProjectID, ev.TaskID), CauseKey: reviewKey(ev.ReviewID),
	}, false)
}

func (s *Service) onSessionStatus(ev domain.SessionStatusChanged) {
	if ev.Status != domain.SessionWaitingApproval {
		s.resolve(sessionWaitingKey(ev.SessionID))
		return
	}
	sess := s.readers.Sessions.Get(ev.SessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", ev.SessionID)
		return
	}
	s.record(&domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionWaiting, Level: domain.NotificationAction,
		Title: "A session is waiting for your approval",
		Body:  clip(sess.LastAction, domain.MaxNotificationBodyLen),
		Link:  domain.TaskLink(sess.ProjectID, sess.TicketID), CauseKey: sessionWaitingKey(sess.ID),
	}, false)
}

func (s *Service) onSessionFailed(sessionID string) {
	sess := s.readers.Sessions.Get(sessionID)
	if sess == nil || sess.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", sessionID)
		return
	}
	s.record(&domain.Notification{
		ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID,
		Kind: domain.NotificationSessionFailed, Level: domain.NotificationInfo,
		Title: "A session failed",
		Body:  clip(sess.LastAction, domain.MaxNotificationBodyLen),
		Link:  domain.TaskLink(sess.ProjectID, sess.TicketID), CauseKey: sessionFailedKey(sess.ID),
	}, true)
}

func (s *Service) onTaskStatus(c domain.TaskStatusChange) {
	if c.Status != domain.TicketStatusReview {
		s.resolve(taskReviewKey(c.TaskID))
		return
	}
	projectID := c.ProjectID
	if projectID == "" {
		if tk := s.readers.Tickets.Get(c.TaskID); tk != nil {
			projectID = tk.ProjectID
		}
	}
	if projectID == "" {
		slog.Warn("notification for a task that is not recorded", "task", c.TaskID)
		return
	}
	s.record(&domain.Notification{
		ProjectID: projectID, TaskID: c.TaskID, SessionID: c.BySessionID,
		Kind: domain.NotificationTaskReview, Level: domain.NotificationAction,
		Title: "A task is ready for your review",
		Body:  clip(strings.TrimSpace(c.Reason), domain.MaxNotificationBodyLen),
		Link:  domain.TaskLink(projectID, c.TaskID), CauseKey: taskReviewKey(c.TaskID),
	}, false)
}

// record stores a system notification unless its cause already has an
// unresolved one (or, with once, any one at all).
func (s *Service) record(n *domain.Notification, once bool) {
	n.Source = domain.NotificationFromSystem
	s.write("record a notification", func(now time.Time) ([]*domain.Notification, bool, error) {
		if once && s.repo.AnyByCause(n.CauseKey) || !once && s.repo.OpenByCause(n.CauseKey) != nil {
			return nil, false, nil
		}
		n.CreatedAt = now
		created, err := s.repo.Create(n)
		if err != nil {
			return nil, false, err
		}
		s.delivery.enqueue(*s.named(created)[0])
		return []*domain.Notification{created}, true, nil
	})
}

// resolve settles the unresolved notifications of a cause.
func (s *Service) resolve(causeKey string) {
	s.write("resolve a notification", func(now time.Time) ([]*domain.Notification, bool, error) {
		changed, err := s.repo.Resolve(causeKey, now)
		return changed, len(changed) > 0, err
	})
}

// write runs change under the write lock and, when it says something
// changed, streams what changed with the unread count as of that commit. An
// error is logged as what failed.
func (s *Service) write(what string, change func(now time.Time) ([]*domain.Notification, bool, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.commit(change); err != nil {
		slog.Warn(what, "err", err)
	}
}

// commit is write's body for callers that already hold s.mu and want the
// change back.
func (s *Service) commit(change func(now time.Time) ([]*domain.Notification, bool, error)) (*ports.NotificationChange, error) {
	changed, ok, err := change(s.now())
	if err != nil || !ok {
		return nil, err
	}
	count, err := s.repo.CountUnread()
	if err != nil {
		return nil, err
	}
	c := ports.NotificationChange{Changed: s.named(changed...), UnreadCount: count}
	if c.Changed == nil {
		c.Changed = []*domain.Notification{}
	}
	s.broadcast(c)
	return &c, nil
}

// broadcast sends c to every follower without waiting: one that is full has
// fallen behind and is closed, so it reloads.
func (s *Service) broadcast(c ports.NotificationChange) {
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
	s.mu.Lock()
	s.followers[ch] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.followers[ch]; ok {
			delete(s.followers, ch)
			close(ch)
		}
	}()
	return ch, nil
}

// ListNotifications answers a page of the inbox, newest first, with the
// global unread count.
func (s *Service) ListNotifications(_ context.Context, q ports.NotificationQuery) (*ports.NotificationPage, error) {
	limit := q.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return nil, invalid(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
	}
	f := ports.NotificationFilter{ProjectID: q.ProjectID, UnreadOnly: q.UnreadOnly, Since: q.Since, Limit: limit + 1}
	if q.Before != "" {
		at, id, err := decodeCursor(q.Before)
		if err != nil {
			return nil, err
		}
		f.BeforeAt, f.BeforeID = at, id
	}
	list, err := s.repo.List(f)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.CountUnread()
	if err != nil {
		return nil, err
	}
	page := &ports.NotificationPage{UnreadCount: count}
	if len(list) > limit {
		list = list[:limit]
		page.NextBefore = encodeCursor(list[limit-1])
	}
	page.Notifications = s.named(list...)
	if page.Notifications == nil {
		page.Notifications = []*domain.Notification{}
	}
	return page, nil
}

// MarkNotificationsRead marks the given ids, or every unread notification
// (of a project), read. An unknown id fails the whole call before anything
// changes. Only the notifications this call changed are answered and
// streamed; providers are not told, since read state is the inbox's.
func (s *Service) MarkNotificationsRead(_ context.Context, req ports.MarkNotificationsRead) (*ports.NotificationChange, error) {
	if (len(req.IDs) > 0) == req.All {
		return nil, invalid("give either ids or all, not both")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range req.IDs {
		if s.repo.Get(id) == nil {
			return nil, &domain.StructuredError{Code: "NOTIFICATION_NOT_FOUND", Message: "notification " + id + " not found"}
		}
	}
	c, err := s.commit(func(now time.Time) ([]*domain.Notification, bool, error) {
		var changed []*domain.Notification
		var err error
		if req.All {
			changed, err = s.repo.MarkAllRead(req.ProjectID, now)
		} else {
			changed, err = s.repo.MarkRead(req.IDs, now)
		}
		return changed, len(changed) > 0, err
	})
	if err != nil {
		return nil, err
	}
	if c == nil {
		count, err := s.repo.CountUnread()
		if err != nil {
			return nil, err
		}
		c = &ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: count}
	}
	return c, nil
}

// Notify records a notification an agent posts, attributed to its session
// (if any), task (if any) and project.
func (s *Service) Notify(_ context.Context, req ports.NotifyRequest) (*domain.Notification, error) {
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
	if s.readers.Projects.Get(req.ProjectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if req.TaskID != "" {
		tk := s.readers.Tickets.Get(req.TaskID)
		if tk == nil {
			return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		if tk.ProjectID != req.ProjectID {
			return nil, &domain.StructuredError{Code: "TASK_NOT_ON_PROJECT", Message: "task " + req.TaskID + " is not in project " + req.ProjectID}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	posted, err := s.repo.CountAgentSince(req.SessionID, req.ProjectID, now.Add(-agentRateWindow))
	if err != nil {
		return nil, err
	}
	if posted >= agentRateLimit {
		who := "this session"
		if req.SessionID == "" {
			who = "this project"
		}
		return nil, &domain.StructuredError{Code: "NOTIFICATION_RATE_LIMITED",
			Message: fmt.Sprintf("%s has posted %d notifications in the last hour, the most allowed; keep notify for what the person has to know or do", who, agentRateLimit)}
	}
	var created *domain.Notification
	_, err = s.commit(func(now time.Time) ([]*domain.Notification, bool, error) {
		var err error
		created, err = s.repo.Create(&domain.Notification{
			ProjectID: req.ProjectID, TaskID: req.TaskID, SessionID: req.SessionID,
			Source: domain.NotificationFromAgent, Kind: domain.NotificationAgent, Level: level,
			Title: title, Body: body, Link: link, CreatedAt: now,
		})
		if err != nil {
			return nil, false, err
		}
		s.delivery.enqueue(*s.named(created)[0])
		return []*domain.Notification{created}, true, nil
	})
	if err != nil {
		return nil, err
	}
	return s.named(created)[0], nil
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
	s.mu.Lock()
	s.delivery.close()
	s.mu.Unlock()
	s.delivery.wait(ctx)
}

// named fills each notification's current project name and task title.
// One lookup per distinct id per call; a deleted task leaves the title out.
func (s *Service) named(list ...*domain.Notification) []*domain.Notification {
	if len(list) == 0 {
		return nil
	}
	projects, tasks := map[string]string{}, map[string]string{}
	out := make([]*domain.Notification, 0, len(list))
	for _, n := range list {
		c := *n
		name, ok := projects[c.ProjectID]
		if !ok {
			if p := s.readers.Projects.Get(c.ProjectID); p != nil {
				name = p.Name
			}
			projects[c.ProjectID] = name
		}
		c.ProjectName = name
		if c.TaskID != "" {
			title, ok := tasks[c.TaskID]
			if !ok {
				if tk := s.readers.Tickets.Get(c.TaskID); tk != nil {
					title = tk.Title
				}
				tasks[c.TaskID] = title
			}
			c.TaskTitle = title
		}
		out = append(out, &c)
	}
	return out
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
