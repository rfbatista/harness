package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// TestCodeViews_WiredServer reads a session's worktree through the wired
// /api/code routes, zoned by the architecture context, and sees an edit in
// it arrive on the project feed as code_changed while the session runs.
func TestCodeViews_WiredServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := portstest.NewCodeFixture(t)
	var (
		cat      catalog.Catalog
		orch     *orchestration.Service
		code     ports.CodeBrowser
		sessions ports.SessionRepository
		wsRepo   ports.WorkspaceRepository
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&cat, &orch, &code, &sessions, &wsRepo),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Code: code, Sessions: orch})))
	t.Cleanup(srv.Close)
	client := httpclient.New(srv.URL)

	p, err := cat.Projects.CreateProject(ctx, "fixture", f.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := cat.Projects.CreateRepository(ctx, p.ID, "fixture", "", "https://example.com/fixture.git", f.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := cat.Architecture.CreateZone(p.ID, "Adapters", "^internal/", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := wsRepo.Create(domain.Workspace{RepositoryID: repo.ID, Name: "session", Branch: f.Branch, Path: f.Worktree, BaseRef: f.BaseRef})
	if err != nil {
		t.Fatal(err)
	}

	tree, err := httpclient.NewCode(client).CodeTree(ctx, ports.CodeSourceQuery{WorkspaceID: ws.ID}, "internal/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Entries) != 2 || !slices.Equal(tree.Entries[1].Zones, []string{zone.ID}) || tree.Entries[1].Change == nil {
		t.Fatalf("wired tree = %+v", tree.Entries)
	}

	// The raw stream, as the browser reads it (the TUI's follower keeps
	// session changes only).
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events?project_id="+p.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	feed := make(chan string, 16)
	go func() {
		scan := bufio.NewScanner(resp.Body)
		for scan.Scan() {
			if data, ok := strings.CutPrefix(scan.Text(), "data: "); ok {
				feed <- data
			}
		}
		close(feed)
	}()
	if _, err := sessions.Create(&domain.Session{ID: "s1", ProjectID: p.ID, WorkspaceID: ws.ID, Status: domain.SessionRunning, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	if err := cat.Bus.Publish(ctx, domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	// Let the watcher see the worktree once, then edit it.
	time.Sleep(3 * time.Second)
	if err := os.WriteFile(filepath.Join(f.Worktree, "README.md"), []byte("hello\nagain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := `{"code_changed":{"workspace_id":"` + ws.ID + `","head":"` + f.SessionHead + `"}}`
	for {
		select {
		case data, ok := <-feed:
			if !ok {
				t.Fatal("the feed closed")
			}
			if !strings.Contains(data, "code_changed") {
				continue
			}
			if data != want {
				t.Fatalf("event = %s, want %s", data, want)
			}
			return
		case <-ctx.Done():
			t.Fatal("no code_changed on the project feed")
		}
	}
}
