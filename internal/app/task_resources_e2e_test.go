package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// wiredResources boots the graph without ExecutionModule (Genkit registers
// flows process-wide) and returns a task to add things to.
// wired is the graph a resources test drives, with a task to add things to.
type wired struct {
	plan     *planning.Service
	orch     *orchestration.Service
	sessions ports.SessionRepository
	channel  *taskchannel.Service
	tk       *domain.Ticket
}

func wiredResources(t *testing.T) wired {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	var (
		cat      catalog.Catalog
		plan     *planning.Service
		orch     *orchestration.Service
		sessions ports.SessionRepository
		channel  *taskchannel.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&cat, &plan, &orch, &sessions, &channel),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	p, err := cat.Projects.CreateProject(ctx, "harness", dir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(ctx, p.ID, "Task", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	return wired{plan, orch, sessions, channel, tk}
}

// On the wired graph an upload keeps its bytes outside any worktree, names
// its content route, goes on the project feed, and goes with its task.
func TestTaskResources_WiredGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := wiredResources(t)
	plan, orch, tk := w.plan, w.orch, w.tk
	feed, err := orch.FollowProject(ctx, tk.ProjectID)
	if err != nil {
		t.Fatal(err)
	}

	r, err := plan.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tk.ID, FileName: "notes.md", Content: strings.NewReader("# Notes")})
	if err != nil {
		t.Fatal(err)
	}
	if r.ContentURL != "/api/ticket_resources/"+r.ID+"/content" || r.Kind != domain.ResourceFile {
		t.Fatalf("resource = %+v", r)
	}
	path, err := plan.TicketResourcePath(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "# Notes" || !strings.Contains(path, "harness-task-resources-") {
		t.Fatalf("bytes at %s = %q, %v", path, b, err)
	}
	c, err := plan.AddTicketComment(ctx, tk.ID, "look at the notes")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < 2 {
		select {
		case ch := <-feed:
			switch {
			case ch.TicketResource != nil:
				got = append(got, "resource "+ch.TicketResource.Resource.ID)
			case ch.TicketComment != nil:
				got = append(got, "comment "+ch.TicketComment.Comment.ID)
			}
		case <-ctx.Done():
			t.Fatalf("feed carried %v", got)
		}
	}
	if got[0] != "resource "+r.ID || got[1] != "comment "+c.ID {
		t.Fatalf("feed = %v", got)
	}

	if err := plan.DeleteTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("bytes outlived their task: %v", err)
	}
}

// On the wired graph what the person adds reaches every running session of
// the task as a [task update · …] turn naming its shell file, through the courier: the architect
// as well as its delegate and a peer. Sessions run by a client take it from their Stop
// hook. A session that has stopped is not a recipient.
func TestTaskUpdates_WiredGraph(t *testing.T) {
	ctx := context.Background()
	w := wiredResources(t)
	plan, orch, sessions, tk := w.plan, w.orch, w.sessions, w.tk
	for _, s := range []*domain.Session{
		{ID: "s-arch", Mode: domain.SessionModeArchitect},
		{ID: "s-dlg", ParentSessionID: "s-arch"},
		{ID: "s-peer"},
		{ID: "s-old", Status: domain.SessionStopped},
	} {
		s.ProjectID, s.TicketID, s.Task, s.Interactive, s.RunsOn = tk.ProjectID, tk.ID, "work", true, domain.RunnerTUI
		if s.Status == "" {
			s.Status = domain.SessionRunning
		}
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	if role, _ := w.channel.SessionRole(ctx, "s-arch"); role != domain.RoleArchitect {
		t.Fatalf("s-arch is %q, want the task's architect", role)
	}
	if role, _ := w.channel.SessionRole(ctx, "s-dlg"); role != domain.RoleDelegate {
		t.Fatalf("s-dlg is %q, want a delegate", role)
	}
	if role, _ := w.channel.SessionRole(ctx, "s-peer"); role != domain.RolePeer {
		t.Fatalf("s-peer is %q, want a peer", role)
	}

	r, err := plan.AddTicketLink(ctx, tk.ID, "https://example.com/spec", "Spec")
	if err != nil {
		t.Fatal(err)
	}
	c, err := plan.AddTicketComment(ctx, tk.ID, "use the spec I linked")
	if err != nil {
		t.Fatal(err)
	}
	// These sessions have no shell: the turns name the files a shell gives
	// a link and the comment.
	commentPath := "task/comments/" + c.CreatedAt.UTC().Format("20060102-150405") + "-person-" + c.ID[:8] + ".md"
	want := `[task update · resource added] The person added a link to this task: "Spec" (` + r.URL + `), ` +
		`listed in task/resources/links.md in your shell. Open it if it bears on your work.` +
		"\n\n---\n\n" +
		"[task update · comment] The person commented on this task (" + commentPath + " in your shell):\nuse the spec I linked"
	for _, id := range []string{"s-arch", "s-dlg", "s-peer"} {
		if got := stopHookUntil(t, orch, id, want); got != want {
			t.Fatalf("%s's Stop hook = %q, want %q", id, got, want)
		}
	}
	if got, err := orch.TurnEnded(ctx, "s-old", false); err != nil || got != "" {
		t.Fatalf("the stopped session had %q queued (%v), want nothing", got, err)
	}
}

// stopHookUntil calls the session's Stop hook until the turns it hands back,
// joined the way the courier joins them, are as long as want, or the
// deadline passes: the drainer delivers asynchronously, so the two updates
// may arrive at one Stop or at two.
func stopHookUntil(t *testing.T, orch *orchestration.Service, id, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var turns []string
	for time.Now().Before(deadline) {
		next, err := orch.TurnEnded(context.Background(), id, false)
		if err != nil {
			t.Fatal(err)
		}
		if next != "" {
			turns = append(turns, next)
		}
		if got := strings.Join(turns, "\n\n---\n\n"); len(got) >= len(want) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return strings.Join(turns, "\n\n---\n\n")
}
