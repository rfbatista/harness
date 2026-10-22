package tooling

import (
	"context"
	"fmt"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// notesFixture is an inbox with a task of project Alpha, a session on it,
// and the session-scoped and global notification tools over it.
type notesFixture struct {
	env     *notificationstest.Env
	svc     *notifications.Service
	task    *domain.Ticket
	session map[string]domain.Tool
	global  map[string]domain.Tool
}

func newNotesFixture(t *testing.T) *notesFixture {
	t.Helper()
	e := notificationstest.NewEnv(t)
	svc := e.Service(t, notifications.Options{})
	plan := planning.NewService(e.Tickets, sqlite.NewDocumentRepository(e.DB), e.Projects)
	tk, err := plan.CreateTicket(context.Background(), e.ProjectA, "Ship", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Sessions.Create(&domain.Session{ID: "sess-1", ProjectID: e.ProjectA, TicketID: tk.ID, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	f := &notesFixture{env: e, svc: svc, task: tk, session: map[string]domain.Tool{}, global: map[string]domain.Tool{}}
	for _, tl := range sessionNotificationTools(e.Sessions, NotificationTooling{Notifier: svc}) {
		f.session[tl.Name] = tl
	}
	for _, tl := range NotificationTools(svc) {
		f.global[tl.Name] = tl
	}
	return f
}

func (f *notesFixture) callSession(name string, args map[string]any) (map[string]any, error) {
	out, err := f.session[name].Handler(WithSessionID(context.Background(), "sess-1"), args)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func (f *notesFixture) callGlobal(name string, args map[string]any) (map[string]any, error) {
	out, err := f.global[name].Handler(context.Background(), args)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

func TestSessionNotifyIsAttributedToTheSessionItsTaskAndProject(t *testing.T) {
	f := newNotesFixture(t)
	out, err := f.callSession("notify", map[string]any{"title": " Need a key ", "body": "for the API", "level": "action"})
	if err != nil {
		t.Fatal(err)
	}
	n := out["notification"].(*domain.Notification)
	if n.SessionID != "sess-1" || n.TaskID != f.task.ID || n.ProjectID != f.env.ProjectA || n.Source != domain.NotificationFromAgent ||
		n.Kind != domain.NotificationAgent || n.Level != domain.NotificationAction || n.Title != "Need a key" ||
		n.Link != domain.TaskLink(f.env.ProjectA, f.task.ID) || n.ProjectName != "Alpha" || n.TaskTitle != "Ship" {
		t.Fatalf("notified = %+v", n)
	}
	out, err = f.callSession("notify", map[string]any{"title": "elsewhere", "link": "/projects/x"})
	if err != nil || out["notification"].(*domain.Notification).Link != "/projects/x" || out["notification"].(*domain.Notification).Level != domain.NotificationInfo {
		t.Fatalf("notify with a link = %+v, %v", out, err)
	}
	for _, args := range []map[string]any{
		{"title": ""}, {"title": "t", "link": "https://evil.example"}, {"title": "t", "level": "loud"},
	} {
		_, err := f.callSession("notify", args)
		wantCode(t, err, "INVALID_INPUT")
	}
}

func TestSessionNotifyIsRateLimited(t *testing.T) {
	f := newNotesFixture(t)
	for i := 0; i < 20; i++ {
		if _, err := f.callSession("notify", map[string]any{"title": fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.callSession("notify", map[string]any{"title": "one too many"})
	wantCode(t, err, "NOTIFICATION_RATE_LIMITED")
}

func TestSessionListNotificationsScopesAndDefaults(t *testing.T) {
	f := newNotesFixture(t)
	ctx := context.Background()
	a, _ := f.svc.Notify(ctx, ports.NotifyRequest{ProjectID: f.env.ProjectA, Title: "a"})
	b, _ := f.svc.Notify(ctx, ports.NotifyRequest{ProjectID: f.env.ProjectB, Title: "b"})
	read, _ := f.svc.Notify(ctx, ports.NotifyRequest{ProjectID: f.env.ProjectA, Title: "read"})
	if _, err := f.svc.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{read.ID}}); err != nil {
		t.Fatal(err)
	}
	ids := func(out map[string]any) []string {
		var got []string
		for _, n := range out["notifications"].([]*domain.Notification) {
			got = append(got, n.ID)
		}
		return got
	}

	out, err := f.callSession("list_notifications", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(out); len(got) != 2 || got[0] != b.ID || got[1] != a.ID || out["count"] != 2 || out["unread_count"] != 2 {
		t.Fatalf("default list = %v %+v, want every project's unread", got, out)
	}
	out, _ = f.callSession("list_notifications", map[string]any{"scope": "project", "unread_only": false})
	if got := ids(out); len(got) != 2 || got[0] != read.ID || got[1] != a.ID || out["unread_count"] != 2 {
		t.Fatalf("project list with read ones = %v", got)
	}
	out, _ = f.callSession("list_notifications", map[string]any{"since": a.CreatedAt.Format("2006-01-02T15:04:05.000Z07:00"), "limit": 1})
	if got := ids(out); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("since a, limit 1 = %v, want b", got)
	}
	for _, args := range []map[string]any{{"scope": "team"}, {"since": "yesterday"}, {"limit": 0}, {"limit": 201}} {
		_, err := f.callSession("list_notifications", args)
		wantCode(t, err, "INVALID_INPUT")
	}
	// Reading marks nothing read.
	if page, _ := f.svc.ListNotifications(ctx, ports.NotificationQuery{}); page.UnreadCount != 2 {
		t.Fatalf("unread after listing = %d", page.UnreadCount)
	}
}

func TestNotificationToolsNeedASessionOnATask(t *testing.T) {
	f := newNotesFixture(t)
	_, err := f.session["notify"].Handler(context.Background(), map[string]any{"title": "t"})
	if err == nil {
		t.Fatal("notify with no session succeeded")
	}
	off := sessionNotificationTools(nil, NotificationTooling{})
	for _, tl := range off {
		_, err := tl.Handler(WithSessionID(context.Background(), "sess-1"), map[string]any{"title": "t"})
		wantCode(t, err, "UNAVAILABLE")
	}
}

func TestGlobalNotificationTools(t *testing.T) {
	f := newNotesFixture(t)
	out, err := f.callGlobal("notify", map[string]any{"project_id": f.env.ProjectA, "title": "board"})
	if err != nil {
		t.Fatal(err)
	}
	n := out["notification"].(*domain.Notification)
	if n.SessionID != "" || n.TaskID != "" || n.Link != domain.ProjectLink(f.env.ProjectA) || n.Source != domain.NotificationFromAgent {
		t.Fatalf("global notify = %+v", n)
	}
	out, err = f.callGlobal("notify", map[string]any{"project_id": f.env.ProjectA, "task_id": f.task.ID, "title": "task"})
	if err != nil || out["notification"].(*domain.Notification).Link != domain.TaskLink(f.env.ProjectA, f.task.ID) {
		t.Fatalf("global notify on a task = %+v, %v", out, err)
	}
	for _, c := range []struct {
		args map[string]any
		code string
	}{
		{map[string]any{"title": "t"}, "INVALID_INPUT"},
		{map[string]any{"project_id": "ghost", "title": "t"}, "PROJECT_NOT_FOUND"},
		{map[string]any{"project_id": f.env.ProjectA, "task_id": "ghost", "title": "t"}, "TICKET_NOT_FOUND"},
		{map[string]any{"project_id": f.env.ProjectB, "task_id": f.task.ID, "title": "t"}, "TASK_NOT_ON_PROJECT"},
	} {
		_, err := f.callGlobal("notify", c.args)
		wantCode(t, err, c.code)
	}
	for i := 0; i < 18; i++ {
		if _, err := f.callGlobal("notify", map[string]any{"project_id": f.env.ProjectA, "title": "t"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.callGlobal("notify", map[string]any{"project_id": f.env.ProjectA, "title": "t"})
	wantCode(t, err, "NOTIFICATION_RATE_LIMITED")
	// A session's posts do not count against the project's global limit.
	if _, err := f.callSession("notify", map[string]any{"title": "mine"}); err != nil {
		t.Fatal(err)
	}

	out, err = f.callGlobal("list_notifications", map[string]any{"project_id": f.env.ProjectB})
	if err != nil || out["count"] != 0 || out["unread_count"] != 21 {
		t.Fatalf("Beta's list = %+v, %v", out, err)
	}
	out, _ = f.callGlobal("list_notifications", map[string]any{"limit": 5})
	if out["count"] != 5 {
		t.Fatalf("limit 5 = %+v", out["count"])
	}
}

// A session with no task (a project-level product session, ADR 0016) can
// still post to the inbox and read it: the notification is attributed to the
// session and its project alone, linked to the project's board.
func TestSessionNotifyWorksInASessionWithoutATask(t *testing.T) {
	f := newNotesFixture(t)
	if _, err := f.env.Sessions.Create(&domain.Session{ID: "free", ProjectID: f.env.ProjectA, Task: "Product: Alpha", Status: domain.SessionRunning, Mode: domain.SessionModeProduct}); err != nil {
		t.Fatal(err)
	}
	ctx := WithSessionID(context.Background(), "free")
	out, err := f.session["notify"].Handler(ctx, map[string]any{"title": "PRD ready", "body": "review the feature task", "level": "action"})
	if err != nil {
		t.Fatal(err)
	}
	n := out.(map[string]any)["notification"].(*domain.Notification)
	if n.SessionID != "free" || n.TaskID != "" || n.ProjectID != f.env.ProjectA || n.Link != domain.ProjectLink(f.env.ProjectA) ||
		n.ProjectName != "Alpha" || n.TaskTitle != "" || n.Level != domain.NotificationAction {
		t.Fatalf("notified = %+v", n)
	}
	listed, err := f.session["list_notifications"].Handler(ctx, map[string]any{"scope": "project"})
	if err != nil {
		t.Fatal(err)
	}
	if got := listed.(map[string]any)["notifications"]; len(got.([]*domain.Notification)) == 0 {
		t.Fatalf("list_notifications without a task = %+v", listed)
	}
}
