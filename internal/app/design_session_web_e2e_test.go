//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

// designSessionPage is the server as it runs with the web UI, a project that
// has a repository and an enabled agent, and one design system, App, at
// version 0. Its fake agent stays up until the session is stopped.
type designSessionPage struct {
	*designPage
	cat   catalog.Catalog
	orch  *orchestration.Service
	agent string
}

func startDesignSessionPage(t *testing.T) *designSessionPage {
	t.Helper()
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &designSessionPage{}
	var plan *planning.Service
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&s.cat, &s.orch, &plan),
		fx.NopLogger,
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	if _, err := s.cat.Settings.UpdateSettings(map[string]string{
		domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees"), orchestration.SettingSandbox: "off",
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.cat.Projects.CreateProject(ctx, "widgets", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "https://example.com/widgets.git", repoDir); err != nil {
		t.Fatal(err)
	}
	ag, err := s.cat.Agents.CreateAgent(ctx, "designer", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.agent = ag.ID
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: s.cat.Projects, Sessions: s.orch, Planning: plan, DesignSystems: s.cat.DesignSystems,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: s.cat.Projects, Tasks: plan, Sessions: s.orch, Agents: s.cat.Agents, Repositories: s.cat.Projects, EnvFiles: s.cat.Projects,
		DesignSystems: s.cat.DesignSystems,
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true // each page holds an event stream open
	srv.StartTLS()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	s.designPage = &designPage{t: t, srv: srv, project: p.ID, url: srv.URL + "/projects/" + p.ID + "/design-systems"}
	s.designPage = s.designPage.with("app", "App", "Marketing and product apps")
	return s
}

// Spawn agent… in the page header asks for an agent and an instruction, warns
// when a design session already runs on the design system and still starts
// another.
func TestSpawnAgentInTheBrowserWarnsOfARunningDesignSessionAndStartsAnother(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	me := newPerson(t)
	spawn := `[data-panel=spawn]`

	me.run(t, "the first start has no warning",
		chromedp.Navigate(s.url),
		designClick(`[data-action=spawn-header]`),
		designPoll(`!!document.querySelector('`+spawn+`')`),
		designPoll(`document.querySelector('`+spawn+` [data-field=agent]').options.length === 1`),
		designPoll(`!document.querySelector('[data-running-warning]')`),
		designPoll(`document.querySelector('`+spawn+` [data-action=start-session]').disabled === true`),
		chromedp.SetValue(spawn+` [data-field=prompt]`, "Set up calm tokens.", chromedp.ByQuery),
		designClick(spawn+` [data-action=start-session]`),
		designPoll(`!!document.querySelector('[data-session-started]')`),
	)
	if got := s.get()["running_design_sessions"]; got != float64(1) {
		t.Fatalf("running_design_sessions = %v, want 1", got)
	}

	me.run(t, "a second start is warned and allowed",
		designClick(`[data-action=spawn-header]`),
		designPoll(`!!document.querySelector('[data-running-warning]')`),
		designPoll(`document.querySelector('[data-running-warning]').textContent.includes('already running')`),
		chromedp.SetValue(spawn+` [data-field=prompt]`, "Try a bolder palette.", chromedp.ByQuery),
		designClick(spawn+` [data-action=start-session]`),
		designPoll(`!!document.querySelector('[data-session-started]')`),
	)
	if got := s.get()["running_design_sessions"]; got != float64(2) {
		t.Fatalf("running_design_sessions = %v, want 2", got)
	}
}

// Ask an agent, the third start of New design system, creates the design
// system at version 0 and opens its page on the spawn form.
func TestAskAnAgentCreatesTheDesignSystemFirstAndOpensTheSpawnForm(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	me := newPerson(t)
	me.run(t, "ask an agent from the list",
		chromedp.Navigate(s.srv.URL+"/projects/"+s.project+"/design-systems"),
		designClick(`[data-action=new-design-system]`),
		designPoll(`!!document.querySelector('[data-new-form] [data-field=name]')`),
		chromedp.SetValue(`[data-new-form] [data-field=name]`, "Garden", chromedp.ByQuery),
		designClick(`[data-new-form] [data-action=continue]`),
		designPoll(`!!document.querySelector('[data-new-form] [data-action=spawn]')`),
		designClick(`[data-new-form] [data-action=spawn]`),
	)
	me.run(t, "the page opens on the spawn form",
		chromedp.WaitVisible(`[data-panel=spawn]`, chromedp.ByQuery),
	)
	list := s.mustPost("list_design_systems", map[string]any{"project_id": s.project})["design_systems"].([]any)
	var found bool
	for _, row := range list {
		r := row.(map[string]any)
		if r["slug"] == "garden" {
			found = r["version"] == float64(0)
		}
	}
	if !found {
		t.Fatalf("design system garden at version 0 not found in %v", list)
	}
}
