//go:build !windows

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// proposedDraft starts a design session on the page's design system, changes
// the signal colour in its draft and submits it: one proposal waits.
func (s *designSessionPage) proposedDraft(summary string) (sessionID string) {
	s.t.Helper()
	ctx := context.Background()
	sess, err := s.orch.StartDesignSession(ctx, s.ds, s.agent, "Calmer signal.")
	if err != nil {
		s.t.Fatal(err)
	}
	view, err := s.cat.DesignSystems.DesignDraftFiles(ctx, sess.ID, s.ds)
	if err != nil || view == nil {
		s.t.Fatalf("draft = %v, %v", view, err)
	}
	for _, f := range view.Files {
		if f.Path != "tokens.json" {
			continue
		}
		changed := strings.Replace(string(f.Content), "oklch(0.5 0.095 182)", "oklch(0.35 0.1 200)", 1)
		if err := s.cat.DesignSystems.WriteDraftFile(ctx, sess.ID, s.ds, "tokens.json", []byte(changed)); err != nil {
			s.t.Fatal(err)
		}
	}
	if _, err := s.cat.DesignSystems.SubmitDesignProposal(ctx, sess.ID, summary); err != nil {
		s.t.Fatal(err)
	}
	return sess.ID
}

// The proposal review: a proposal opens, the page, the tab and the navigation
// item say so, the review shows each change with before and after previews,
// and Approve all writes one version the page then shows.
func TestProposalReviewInTheBrowserApprovesAProposalAsOneVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	me := newPerson(t)
	me.run(t, "the page before any proposal",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(s.url),
		designPoll(`document.querySelector('[data-design-system-name]')?.textContent.trim() === 'App'`),
		designPoll(`!!document.querySelector('[data-tab-count]') && !document.querySelector('[data-proposal-banner]')`),
		designPoll(`document.querySelector('[data-nav-proposals]')?.hidden === true`),
	)

	// The proposal arrives while the page is open: banner, tab count and badge follow the feed.
	s.proposedDraft("Calmer signal colour")
	me.run(t, "the banner, the tab count and the navigation badge",
		designPoll(`!!document.querySelector('[data-proposal-banner]')`),
		designPoll(`document.querySelector('[data-proposal-banner-text]').textContent.includes('designer') && document.querySelector('[data-proposal-banner-text]').textContent.includes('Calmer signal colour')`),
		designPoll(`document.querySelector('[data-nav-proposals]').hidden === false && document.querySelector('[data-nav-proposals]').textContent.trim() === '1'`),
		designPoll(`[...document.querySelectorAll('#design-system-tabs [role=tab]')].find((b) => b.textContent.includes('Proposals'))?.textContent.includes('1')`),
		shot("proposals-banner", "light"),
	)

	me.run(t, "the review",
		chromedp.EmulateViewport(1280, 1700),
		designClick(`[data-action=review-proposal]`),
		designPoll(`!!document.querySelector('[data-proposal-review]')`),
		designPoll(`document.querySelector('[data-review-summary]').textContent === 'Calmer signal colour'`),
		designPoll(`document.querySelectorAll('[data-proposal-panel]').length === 1`),
		designPoll(`document.querySelector('[data-token-compare]').textContent.includes('oklch(0.35 0.1 200)') && document.querySelector('[data-token-compare]').textContent.includes('oklch(0.5 0.095 182)')`),
		designPoll(`document.querySelector('[data-contrast-before]')?.textContent.endsWith(':1') && document.querySelector('[data-contrast-after]')?.textContent.endsWith(':1')`),
		// The button's two previews each get a before and an after frame.
		designPoll(`document.querySelectorAll('[data-preview-pair] iframe').length === 4`),
		designPoll(`[...document.querySelectorAll('[data-preview-pair] iframe')].every((f) => f.getAttribute('sandbox') === '' && f.srcdoc.includes('--colors-signal'))`),
		designPoll(`document.querySelector('[data-next-version]').textContent.includes('version 2')`),
		designPoll(`!document.querySelector('[data-proposal-banner]')`),
		shot("proposals-review", "light"),
	)

	me.run(t, "approve",
		designClick(`[data-action=approve-all]`),
		designPoll(`document.querySelector('[data-proposal-result]')?.textContent.includes('version 2')`),
		designPoll(`document.querySelector('[data-proposal-row] [data-proposal-state]')?.textContent.trim() === 'Approved'`),
		designPoll(`document.querySelector('[data-nav-proposals]').hidden === true`),
		shot("proposals-approved", "light"),
	)
	got := s.get()
	if got["version"] != float64(2) || got["updated_by"] == "person" || got["open_proposals"] != float64(0) {
		t.Fatalf("design system = %v", got)
	}
	me.run(t, "the page shows the new version",
		chromedp.Navigate(s.url+"?tab=history"),
		designPoll(`document.querySelector('[data-version-row="2"]')?.textContent.includes('Agent proposal')`),
	)
}

// A proposal that overwrites something the person saved since its base cannot
// be approved until each overwrite is confirmed.
func TestProposalReviewInTheBrowserAsksToConfirmEachOverwrite(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	s := startDesignSessionPage(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	s.proposedDraft("Calmer signal colour")
	record := dsRecord(buttonComponent())
	record["tokens"].([]any)[0] = dsToken("colors", "signal", "color", "oklch(0.6 0.2 30)")
	s.mustSave(1, record)

	me := newPerson(t)
	me.run(t, "a conflicting proposal cannot be approved unconfirmed",
		chromedp.EmulateViewport(1280, 1700),
		chromedp.Navigate(s.url+"?tab=proposals"),
		designPoll(`!!document.querySelector('[data-proposal-row]')`),
		designPoll(`document.querySelector('[data-proposal-detail]').textContent.includes('1 conflict')`),
		designClick(`[data-action=open-proposal]`),
		designPoll(`!!document.querySelector('[data-proposal-conflict]')`),
		designPoll(`document.querySelector('[data-proposal-conflict]').textContent.includes('version 2')`),
		designPoll(`document.querySelector('[data-action=approve-all]').disabled === true`),
		shot("proposals-conflict", "light"),
		designClick(`[data-field=confirm-overwrite]`),
		designPoll(`document.querySelector('[data-action=approve-all]').disabled === false`),
		designClick(`[data-action=approve-all]`),
		designPoll(`document.querySelector('[data-proposal-result]')?.textContent.includes('version 3')`),
	)
	if got := s.get(); got["version"] != float64(3) {
		t.Fatalf("design system = %v", got)
	}
}
