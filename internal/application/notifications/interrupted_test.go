package notifications_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// agentNames reads an agent's name the way the app's catalog does.
type agentNames map[string]string

func (a agentNames) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	return &domain.Agent{ID: id, Name: a[id]}, nil
}

func newInterruptedRig(t *testing.T) *rig {
	t.Helper()
	e := notificationstest.NewEnv(t)
	readers := e.Readers()
	readers.Agents = agentNames{"a1": "go-developer", "a2": "devops"}
	svc, err := notifications.New(e.Repo, readers, notifications.Options{Clock: e.Clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	svc.Subscribe(bus)
	return &rig{Env: e, svc: svc, bus: bus}
}

// interrupt stores a session as a restart left it: stopped, with the mark.
func (r *rig) interrupt(t *testing.T, id, projectID, taskID, agentID string) {
	t.Helper()
	if _, err := r.Sessions.Create(&domain.Session{ID: id, ProjectID: projectID, TicketID: taskID, AgentID: agentID, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	r.mark(t, id)
}

// mark gives a stored session the interrupted mark and records it stopped.
func (r *rig) mark(t *testing.T, id string) {
	t.Helper()
	at := time.Now()
	if err := r.Sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 1, Type: "interrupted_by_restart", Interrupted: &at, Status: domain.SessionStopped}); err != nil {
		t.Fatal(err)
	}
}

// resume clears the mark as a resume does, and announces the status moving.
func (r *rig) resume(t *testing.T, id string) {
	t.Helper()
	if err := r.Sessions.RecordEvent(id, ports.SessionEventRecord{Seq: 2, Type: "resumed_with_continue_turn", ClearInterrupted: true, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	r.publish(t, domain.SessionStatusChanged{SessionID: id, Status: domain.SessionRunning})
}

func (r *rig) interruptedNotifications(t *testing.T) []*domain.Notification {
	t.Helper()
	var out []*domain.Notification
	for _, n := range r.list(t, ports.NotificationQuery{}).Notifications {
		if n.Kind == domain.NotificationSessionsInterrupted {
			out = append(out, n)
		}
	}
	return out
}

func stateOf(n *domain.Notification, sessionID string) domain.InterruptedSessionState {
	for _, s := range n.InterruptedSessions {
		if s.SessionID == sessionID {
			return s.State
		}
	}
	return ""
}

func TestABootThatFindsInterruptedSessionsPostsOneNotificationListingThem(t *testing.T) {
	r := newInterruptedRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	r.interrupt(t, "s1", r.ProjectA, tk.ID, "a1")
	r.interrupt(t, "s2", r.ProjectB, "", "a2")
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1", "s2"}})

	got := r.interruptedNotifications(t)
	if len(got) != 1 {
		t.Fatalf("%d sessions_interrupted notifications, want 1", len(got))
	}
	n := got[0]
	if n.Level != domain.NotificationAction || n.Source != domain.NotificationFromSystem || n.Link != "/notifications" ||
		n.Title != "2 sessions interrupted by a restart" || n.ProjectID != r.ProjectA || n.TaskID != "" || n.SessionID != "" {
		t.Errorf("notification = %+v", n)
	}
	if want := "Ship · go-developer\n"; !strings.HasPrefix(n.Body, want) || !strings.Contains(n.Body, "devops") {
		t.Errorf("body = %q, want a line per session starting %q", n.Body, want)
	}
	if len(n.InterruptedSessions) != 2 {
		t.Fatalf("interrupted_sessions = %+v", n.InterruptedSessions)
	}
	first := n.InterruptedSessions[0]
	if first.SessionID != "s1" || first.ProjectID != r.ProjectA || first.TaskID != tk.ID || first.TaskTitle != "Ship" ||
		first.AgentName != "go-developer" || first.State != domain.InterruptedSessionInterrupted ||
		first.Link != domain.TaskLink(r.ProjectA, tk.ID)+"?session=s1" {
		t.Errorf("first entry = %+v", first)
	}
}

func TestABootThatFindsNoInterruptedSessionPostsNothing(t *testing.T) {
	r := newInterruptedRig(t)
	r.session(t, "s1", r.ProjectA, "") // running, not marked
	r.publish(t, domain.SessionsInterrupted{SessionIDs: nil})
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1"}})
	if got := r.interruptedNotifications(t); len(got) != 0 {
		t.Fatalf("%d notifications, want none", len(got))
	}
}

func TestANewBootListsTheSessionsStillMarkedAndResolvesTheEarlierNotification(t *testing.T) {
	r := newInterruptedRig(t)
	r.interrupt(t, "s1", r.ProjectA, "", "a1")
	r.interrupt(t, "s2", r.ProjectA, "", "a1")
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1"}})
	r.interrupt(t, "s3", r.ProjectA, "", "a2")
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1", "s2", "s3"}})

	var open []*domain.Notification
	for _, n := range r.interruptedNotifications(t) {
		if n.ResolvedAt == nil {
			open = append(open, n)
		}
	}
	if len(open) != 1 || len(open[0].InterruptedSessions) != 3 {
		t.Fatalf("open = %+v, want one listing 3 sessions", open)
	}
	if page := r.list(t, ports.NotificationQuery{}); len(page.Notifications) != 2 {
		t.Errorf("%d notifications, want 2 (one resolved, one open)", len(page.Notifications))
	}
}

func TestAListedSessionResumedByHandShowsResumedAndTheLastOneResolvesTheNotification(t *testing.T) {
	r := newInterruptedRig(t)
	r.interrupt(t, "s1", r.ProjectA, "", "a1")
	r.interrupt(t, "s2", r.ProjectA, "", "a1")
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1", "s2"}})
	changes, err := r.svc.FollowNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}

	r.resume(t, "s1")
	c := <-changes
	if len(c.Changed) != 1 || stateOf(c.Changed[0], "s1") != domain.InterruptedSessionResumed ||
		stateOf(c.Changed[0], "s2") != domain.InterruptedSessionInterrupted || c.Changed[0].ResolvedAt != nil {
		t.Fatalf("after the first resume the change = %+v", c.Changed)
	}
	r.resume(t, "s2")
	c = <-changes
	if len(c.Changed) != 1 || c.Changed[0].ResolvedAt == nil || stateOf(c.Changed[0], "s2") != domain.InterruptedSessionResumed {
		t.Fatalf("after the last resume the change = %+v, want it resolved", c.Changed)
	}
	if c.UnreadCount != 0 {
		t.Errorf("unread = %d, want 0", c.UnreadCount)
	}
}

func TestADeletedListedSessionIsGoneAndNoMarkedSessionLeftResolvesTheNotification(t *testing.T) {
	r := newInterruptedRig(t)
	r.interrupt(t, "s1", r.ProjectA, "", "a1")
	r.publish(t, domain.SessionsInterrupted{SessionIDs: []string{"s1"}})
	if err := r.Sessions.Delete("s1"); err != nil {
		t.Fatal(err)
	}
	r.publish(t, domain.SessionDeleted{SessionID: "s1"})

	got := r.interruptedNotifications(t)
	if len(got) != 1 || got[0].ResolvedAt == nil || stateOf(got[0], "s1") != domain.InterruptedSessionGone {
		t.Fatalf("notification = %+v, want it resolved with s1 gone", got)
	}
}
