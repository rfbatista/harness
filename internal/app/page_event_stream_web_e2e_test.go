//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The connection budget, end to end (ADR 0011): the server as it runs, on a
// plain HTTP/1.1 listener as the person meets it, and one headless Chrome
// whose tabs share Chrome's six connections per host. The other web e2e
// tests serve TLS with HTTP/2, which lifts the budget and hid the hang.

type budgetWeb struct {
	ctx     context.Context
	orch    *orchestration.Service
	srv     *httptest.Server
	project string
	repo    string
	ticket  string
}

func newBudgetWeb(t *testing.T) *budgetWeb {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
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
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &budgetWeb{ctx: context.Background()}
	var (
		cat     catalog.Catalog
		plan    *planning.Service
		channel *taskchannel.Service
		runner  *apps.Service
		art     *artifacts.Service
		notes   *notifications.Service
	)
	// Without ExecutionModule: it registers Genkit flows in a process-wide
	// registry, so it cannot boot twice in one test binary.
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule, NotificationsModule,
		fx.Populate(&e.orch, &plan, &channel, &cat, &runner, &art, &notes),
		fx.NopLogger,
	)
	if err := app.Start(e.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(e.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(e.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(e.ctx, p.ID, "Application being frozen", "Two tabs on one task hang.", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo, e.ticket = p.ID, r.ID, tk.ID
	// The shell standing in for claude writes no transcript to resume from.
	e.orch.Transcripts = nil

	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, ProjectFeed: cat.Projects, Agents: cat.Agents, Settings: cat.Settings, Sessions: e.orch, Planning: plan,
		TaskChannel: channel, RunCommands: cat.Projects, Apps: runner, Artifacts: art, Notifications: notes,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: e.orch, Agents: cat.Agents, Repositories: cat.Projects,
		EnvFiles: cat.Projects, Documents: plan, Artifacts: art, Notifications: notes,
	}, assets, nil))
	// Plain HTTP/1.1, as the server runs: no TLS, so no HTTP/2.
	e.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		e.srv.CloseClientConnections() // the pages' event streams never end on their own
		e.srv.Close()
	})
	return e
}

// start starts an interactive session on the task: a shell standing in for claude.
func (e *budgetWeb) start(t *testing.T, prompt string) *domain.Session {
	t.Helper()
	s, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket, Prompt: prompt, RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 100, Rows: 30}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.orch.Stop(context.Background(), s.ID) })
	return s
}

// waitFor fails the test when the session's status does not satisfy ok within 5s.
func (e *budgetWeb) waitFor(t *testing.T, id string, ok func(domain.SessionStatus) bool, step string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := e.orch.Get(e.ctx, id)
		if err == nil && ok(s.Status) {
			return
		}
		if time.Now().After(deadline) {
			if err == nil {
				t.Fatalf("%s: still %s after 5s", step, s.Status)
			}
			t.Fatalf("%s: %v", step, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (e *budgetWeb) taskURL(sessionID string) string {
	return fmt.Sprintf("%s/projects/%s/tasks/%s?session=%s", e.srv.URL, e.project, e.ticket, sessionID)
}

// countStreams wraps EventSource before any page script runs, so the page
// can say how many long-lived streams it holds open.
const countStreams = `(() => {
  const Real = window.EventSource;
  const open = new Set();
  window.__streams = () => [...open].filter(s => s.readyState !== 2).map(s => s.url);
  window.EventSource = class extends Real {
    constructor(...args) { super(...args); open.add(this); }
    close() { open.delete(this); super.close(); }
  };
})();`

// budgetTab is one tab of the person's browser: all tabs share one profile,
// and with it the connection budget.
type budgetTab struct {
	ctx context.Context
}

func newBudgetTabs(t *testing.T, n int) []*budgetTab {
	t.Helper()
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-extensions", true))...)
	browser, cancelBrowser := chromedp.NewContext(alloc)
	t.Cleanup(func() { cancelBrowser(); cancelAlloc() })
	if err := chromedp.Run(browser); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || strings.Contains(err.Error(), "executable file not found") {
			t.Skipf("browser test: Chrome not found (%v)", err)
		}
		t.Fatal(err)
	}
	tabs := []*budgetTab{{ctx: browser}}
	for len(tabs) < n {
		ctx, cancel := chromedp.NewContext(browser) // a new tab in the same browser
		t.Cleanup(cancel)
		if err := chromedp.Run(ctx); err != nil {
			t.Fatal(err)
		}
		tabs = append(tabs, &budgetTab{ctx: ctx})
	}
	for _, tab := range tabs {
		if err := chromedp.Run(tab.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(countStreams).Do(ctx)
			return err
		})); err != nil {
			t.Fatal(err)
		}
	}
	return tabs
}

// within runs actions on the tab and fails the test with step when they take
// longer than d.
func (tab *budgetTab) within(t *testing.T, d time.Duration, step string, actions ...chromedp.Action) {
	t.Helper()
	ctx, cancel := context.WithTimeout(tab.ctx, d)
	defer cancel()
	start := time.Now()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("%s: no answer after %s (%v)", step, time.Since(start).Round(time.Millisecond), err)
	}
}

// open navigates without waiting for the load event: a starved tab may never
// fire it, and the test then fails on what it waits for next.
func (tab *budgetTab) open(t *testing.T, step, url string) {
	t.Helper()
	tab.within(t, 5*time.Second, step, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(url).Do(ctx)
		return err
	}))
}

// until is true once js is, evaluated afresh each time: a poll started
// before a navigation commits would stay on the page it replaced.
func until(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for {
			var ok bool
			if err := chromedp.Evaluate(js, &ok).Do(ctx); err == nil && ok {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
}

func (tab *budgetTab) streams(t *testing.T) []string {
	t.Helper()
	var urls []string
	tab.within(t, 5*time.Second, "count the page's streams", chromedp.Evaluate(`window.__streams ? window.__streams() : []`, &urls))
	return urls
}

const (
	pageReady      = `!!document.querySelector('[role=listbox] .row[aria-selected=true]') && !document.querySelector('[role=listbox] [data-ssr]')`
	resumeSelected = `document.querySelector('[role=listbox] .row[aria-selected=true] button.action')`
	historyAnswer  = `(() => { const h = document.querySelector('section[aria-label=History]'); return !!h && !(h.querySelector('.empty[role=status]')?.textContent ?? '').includes('Loading'); })()`
)

func TestThreeTabsOnOneTaskResumeAndOpenHistoryPromptly_EndToEnd(t *testing.T) {
	e := newBudgetWeb(t)
	live := e.start(t, "Keep running.")
	ended := e.start(t, "Run, then stop.")
	if err := e.orch.Stop(e.ctx, ended.ID); err != nil {
		t.Fatal(err)
	}
	e.waitFor(t, ended.ID, func(s domain.SessionStatus) bool { return s == domain.SessionStopped }, "the session stops")

	tabs := newBudgetTabs(t, 3)
	// Two tabs on the task, each with the live session selected: every
	// stream they hold stays open.
	for i, tab := range tabs[:2] {
		tab.open(t, fmt.Sprintf("tab %d loads the task", i+1), e.taskURL(live.ID))
		tab.within(t, 15*time.Second, fmt.Sprintf("tab %d shows the task", i+1), until(pageReady))
	}

	// In the third tab, the person resumes the ended session and opens its
	// History: both answer within a few seconds.
	last := tabs[2]
	last.open(t, "the third tab loads the task", e.taskURL(ended.ID))
	last.within(t, 5*time.Second, "the third tab shows the task", until(pageReady))
	last.within(t, 5*time.Second, "the third tab offers Resume", until(`!!`+resumeSelected+` && !`+resumeSelected+`.disabled`))
	last.within(t, time.Second, "click Resume", chromedp.Evaluate(resumeSelected+`.click()`, nil))
	e.waitFor(t, ended.ID, func(s domain.SessionStatus) bool { return s != domain.SessionStopped }, "resume from the third tab reaches the server")
	last.within(t, 5*time.Second, "the third tab shows the session resumed", until(`!`+resumeSelected+` && !!document.querySelector('[role=tab][aria-selected=true]')?.textContent.trim().startsWith('Agent')`))
	last.within(t, 5*time.Second, "open History in the third tab",
		until(`[...document.querySelectorAll('[role=tab]')].some(b => b.textContent.trim().startsWith('History'))`),
		clickTab("History"), until(historyAnswer))

	// Every tab holds exactly one long-lived stream.
	for i, tab := range tabs {
		if got := tab.streams(t); len(got) != 1 {
			t.Errorf("tab %d holds %d streams, want 1: %v", i+1, len(got), got)
		}
	}
}
