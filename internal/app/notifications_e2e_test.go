package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// logLines records what is logged, for the log provider's lines.
type logLines struct {
	mu    sync.Mutex
	lines []map[string]string
}

func (l *logLines) Enabled(context.Context, slog.Level) bool { return true }
func (l *logLines) Handle(_ context.Context, r slog.Record) error {
	line := map[string]string{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		line[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	return nil
}
func (l *logLines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logLines) WithGroup(string) slog.Handler      { return l }

// delivered is the ids the log provider wrote, in order.
func (l *logLines) delivered() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []string
	for _, line := range l.lines {
		if line["msg"] == "notification" {
			ids = append(ids, line["id"])
		}
	}
	return ids
}

func (l *logLines) waitDelivered(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range l.delivered() {
			if got == id {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the log provider never delivered %s; it delivered %v", id, l.delivered())
}

// The wired server: a review request reaches the inbox, the stream and the
// log provider, and answering it resolves it; a session's notify over its MCP
// endpoint lands in the inbox; turning the log provider off through the
// settings API stops its deliveries.
func TestNotifications_WiredServer(t *testing.T) {
	logs := &logLines{}
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		channel  *taskchannel.Service
		sessions ports.SessionRepository
		notes    *notifications.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&cat, &plan, &channel, &sessions, &notes),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Notifications: notes, Settings: cat.Settings, Planning: plan, TaskChannel: channel,
	})))
	mux.Handle(mcpsession.PathPrefix, mcpsession.TaskHandler(tooling.SessionTaskTools(plan, sessions, nil, nil,
		tooling.PeerStarter{}, tooling.ArtifactTooling{}, channel, tooling.NotificationTooling{Notifier: notes})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	inbox := httpclient.NewNotifications(httpclient.New(srv.URL))

	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(ctx, p.ID, "Notifications", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(&domain.Session{ID: "arch", ProjectID: p.ID, TicketID: tk.ID, Mode: domain.SessionModeArchitect, Status: domain.SessionRunning, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	if err := cat.Bus.Publish(ctx, domain.SessionStarted{SessionID: "arch", ProjectID: p.ID, TicketID: tk.ID, Mode: domain.SessionModeArchitect, StatusCheckMinutes: new(int)}); err != nil {
		t.Fatal(err)
	}

	changes, err := inbox.FollowNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := func() ports.NotificationChange {
		t.Helper()
		select {
		case c, ok := <-changes:
			if !ok {
				t.Fatal("the stream closed")
			}
			return c
		case <-ctx.Done():
			t.Fatal("no change on the stream")
		}
		return ports.NotificationChange{}
	}

	// A review request: inbox, stream, log provider.
	r, err := channel.RequestUserReview(ctx, "arch", ports.ReviewRequestInput{Subject: "the plan", Body: "please look"})
	if err != nil {
		t.Fatal(err)
	}
	c := next()
	if len(c.Changed) != 1 || c.UnreadCount != 1 {
		t.Fatalf("change after the review request = %+v", c)
	}
	n := c.Changed[0]
	if n.Kind != domain.NotificationReviewRequested || n.Level != domain.NotificationAction || n.Title != "Review requested: the plan" ||
		n.ProjectName != "harness" || n.TaskTitle != "Notifications" || n.Link != domain.TaskLink(p.ID, tk.ID) {
		t.Fatalf("review notification = %+v", n)
	}
	page, err := inbox.ListNotifications(ctx, ports.NotificationQuery{UnreadOnly: true})
	if err != nil || len(page.Notifications) != 1 || page.Notifications[0].ID != n.ID {
		t.Fatalf("inbox = %+v, %v", page, err)
	}
	logs.waitDelivered(t, n.ID)

	// Answering it resolves it.
	if _, _, err := channel.RespondReview(ctx, r.ID, domain.ReviewApproved, ""); err != nil {
		t.Fatal(err)
	}
	c = next()
	if len(c.Changed) != 1 || c.Changed[0].ID != n.ID || c.Changed[0].ResolvedAt == nil || c.UnreadCount != 0 {
		t.Fatalf("change after the answer = %+v", c)
	}

	// A session's notify over its MCP endpoint.
	mcp := dialTask(t, srv.URL+mcpsession.PathPrefix+"arch")
	var call mcplib.CallToolRequest
	call.Params.Name = "notify"
	call.Params.Arguments = map[string]any{"title": "Need a decision", "level": "action"}
	res, err := mcp.CallTool(ctx, call)
	if err != nil || res.IsError {
		t.Fatalf("notify over MCP = %+v, %v", res, err)
	}
	var posted struct{ Notification domain.Notification }
	if err := json.Unmarshal([]byte(res.Content[0].(mcplib.TextContent).Text), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.Notification.SessionID != "arch" || posted.Notification.TaskID != tk.ID || posted.Notification.Source != domain.NotificationFromAgent {
		t.Fatalf("posted = %+v", posted.Notification)
	}
	if c = next(); len(c.Changed) != 1 || c.Changed[0].ID != posted.Notification.ID || c.UnreadCount != 1 {
		t.Fatalf("change after notify = %+v", c)
	}
	logs.waitDelivered(t, posted.Notification.ID)

	// Turning the log provider off through the settings API stops delivery.
	resp, err := http.Post(srv.URL+"/api/update_settings", "application/json",
		strings.NewReader(`{"settings":{"notifications.log.enabled":"false"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update_settings = %d", resp.StatusCode)
	}
	resp, err = http.Post(srv.URL+"/api/update_settings", "application/json",
		strings.NewReader(`{"settings":{"notifications.log.enabled":"maybe"}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("update_settings with a bad value = %d, want 400", resp.StatusCode)
	}
	quiet, err := notes.Notify(ctx, ports.NotifyRequest{ProjectID: p.ID, Title: "nobody logs this"})
	if err != nil {
		t.Fatal(err)
	}
	if c = next(); len(c.Changed) != 1 || c.Changed[0].ID != quiet.ID {
		t.Fatalf("change after the quiet notify = %+v", c)
	}
	time.Sleep(100 * time.Millisecond)
	for _, id := range logs.delivered() {
		if id == quiet.ID {
			t.Fatal("the log provider delivered with notifications.log.enabled false")
		}
	}
}
