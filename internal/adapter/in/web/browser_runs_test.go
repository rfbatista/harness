package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/agents/command"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/application/apps"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// noRunCommands is a repository with no saved run commands.
type noRunCommands struct{}

func (noRunCommands) ListRunCommands(context.Context, string) ([]*domain.RunCommand, error) {
	return []*domain.RunCommand{}, nil
}
func (noRunCommands) SaveRunCommand(context.Context, string, string, string) (*domain.RunCommand, error) {
	return nil, &domain.StructuredError{Code: "INTERNAL", Message: "not in this test"}
}
func (noRunCommands) DeleteRunCommand(context.Context, string, string) error { return nil }

// TestAppTabRunsACommandInTheWorktree runs a real command, on a real PTY, in
// a temp dir standing in for the session's worktree: the App tab types it,
// the output shows in xterm with its colors, Stop ends it.
func TestAppTabRunsACommandInTheWorktree(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	session := &domain.Session{
		ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "implement this task",
		Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
		WorkingDir: t.TempDir(), Branch: "agent/add-sse-feed-1a2b",
	}
	sessions := fakeSessions{[]*domain.Session{session}}
	host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	runner := apps.NewService(sessions, noRunCommands{}, host)

	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:     sessions,
	}, assets, nil)
	api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{RunCommands: noRunCommands{}, Apps: runner}))
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler()) // the agent's terminal is not under test
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(withStream(mux))
	defer srv.Close()

	rows := `document.querySelector('[aria-label=Application] .terminal .xterm-rows')?.textContent ?? ''`
	// The ANSI color reaches xterm as a style, not as text.
	colored := `[...document.querySelectorAll('[aria-label=Application] .xterm-rows span')].some(s => s.textContent.includes('app is up') && [...s.classList].some(c => c.startsWith('xterm-fg-')))`
	var runState, afterStop, source, worktree, problems string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		clickButton(`[role=tablist]`, "App"),
		chromedp.Poll(`!!document.querySelector('#run-choice-input')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		setField(`#run-choice-input`, `printf '\033[32mapp is up\033[0m in %s\n' "$(basename "$PWD")"; sleep 30`, "input"),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .source p').textContent.replace(/\s+/g, ' ').trim()`, &source),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .source .code').textContent`, &worktree),
		clickButton(`[aria-label=Application]`, "Run"),
		chromedp.Poll(colored, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(400*time.Millisecond), // the resize debounce
		chromedp.Evaluate(paneProblems(`[aria-label=Application]`), &problems),
		chromedp.FullScreenshot(&shot, 80),
		clickButton(`[aria-label=Application]`, "Stop"),
		chromedp.Poll(`document.querySelector('[aria-label=Application] .repel .status')?.textContent === 'stopped'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .repel .status')?.textContent ?? ''`, &runState),
		// The terminal reports the exit; the output stays on screen.
		chromedp.Poll(terminalDot+`?.getAttribute('aria-label')?.startsWith('Exited with code') ?? false`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &afterStop),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if problems != "" {
		t.Errorf("the run terminal does not fit its pane (layout contract L3, L6):\n%s", problems)
	}
	if want := "Runs harness on branch agent/add-sse-feed-1a2b — this session's worktree, with the agent's changes, not your own checkout."; source != want {
		t.Errorf("source line = %q\nwant          %q", source, want)
	}
	if worktree != session.WorkingDir {
		t.Errorf("worktree = %q, want %q", worktree, session.WorkingDir)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/app-tab.jpg", shot, 0o644)
	}
	if runState != "stopped" {
		t.Errorf("run state = %q, want stopped", runState)
	}
	if !strings.Contains(afterStop, "app is up in "+filepath.Base(session.WorkingDir)) {
		t.Errorf("after stopping, the screen lost the output: %q", afterStop)
	}
	runs, _ := runner.List(context.Background(), "s1")
	if len(runs) != 1 || runs[0].Dir != session.WorkingDir || runs[0].Status != domain.AppRunStopped {
		t.Fatalf("runs = %+v", runs)
	}
	var _ ports.AppRunner = runner
}

// TestAppTabTypingACommandEnablesRunAndSavesIt types a command with real keys
// in a repository that already has a saved command, as the person does: Run
// enables, "Save for this repository as" appears and saves it, and the saved
// command, picked and run, shows its output. At desktop and phone widths
// (on a phone the combobox's input moves into a sheet).
func TestAppTabTypingACommandEnablesRunAndSavesIt(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int64
	}{{"desktop", 1280, 800}, {"phone", 390, 844}} {
		t.Run(size.name, func(t *testing.T) {
			assets, err := NewAssets()
			if err != nil {
				t.Fatal(err)
			}
			if !assets.Built() {
				t.Skip("browser test: web client not built; run `make web`")
			}
			ctx, errs := browser(t)

			session := &domain.Session{
				ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "implement this task",
				Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
				WorkingDir: t.TempDir(), Branch: "agent/add-sse-feed-1a2b",
			}
			sessions := fakeSessions{[]*domain.Session{session}}
			commands := &memRunCommands{byKey: map[string]string{"r1\x00build": "echo building"}}
			host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
			t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
			runner := apps.NewService(sessions, commands, host)

			pages := NewHandler(Deps{
				Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
				Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
				Agents:       fakeAgents{},
				Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
				Sessions:     sessions,
			}, assets, nil)
			api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{RunCommands: commands, Apps: runner}))
			mux := http.NewServeMux()
			mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler())
			mux.Handle("/api/", api)
			mux.Handle("/", pages)
			srv := httptest.NewServer(withStream(mux))
			defer srv.Close()

			panel := `[aria-label=Application]`
			runEnabled := `!document.querySelector('` + panel + ` form button[type=submit]').disabled`
			saveForm := `!!document.querySelector('#run-save-name')`
			var enabled, offered bool
			step := func(name string, actions ...chromedp.Action) {
				t.Helper()
				if err := chromedp.Run(ctx, actions...); err != nil {
					t.Fatalf("%s: %v\nJS errors: %v", name, err, errs.all())
				}
			}
			step("open the App tab",
				chromedp.EmulateViewport(size.w, size.h),
				chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
				// Below 64rem the list shows alone until a session is picked.
				chromedp.Poll(`!document.querySelector('[data-ssr]') && !!document.querySelector('[role=option][data-session-id="s1"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(`document.querySelector('[role=option][data-session-id="s1"]').click()`, nil),
				chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				clickButton(`[role=tablist]`, "App"),
				chromedp.Poll(`document.querySelector('#run-choice-input')?.value === 'build'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			)
			step("type a command",
				chromedp.Click(`#run-choice-input`, chromedp.ByQuery),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`document.querySelector('#run-choice-input').select()`, nil),
				chromedp.KeyEvent(`echo served from "$(basename "$PWD")"`),
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(runEnabled, &enabled),
				chromedp.Evaluate(saveForm, &offered),
			)
			if !enabled || !offered {
				t.Fatalf("after typing a command: Run enabled = %v, save form offered = %v; want both", enabled, offered)
			}
			step("save it",
				chromedp.KeyEvent(kb.Escape), // closes the suggestions (and a phone's sheet)
				chromedp.SendKeys(`#run-save-name`, "serve", chromedp.ByQuery),
				clickButton(panel+` form:has(#run-save-name)`, "Save"),
				chromedp.Poll(`document.querySelector('`+panel+`')?.textContent.includes('Saved “serve” for this repository.')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			)
			if got, _ := commands.snapshot(); len(got) != 2 || got["serve"] != `echo served from "$(basename "$PWD")"` {
				t.Fatalf("saved run commands = %v, want build and serve", got)
			}
			var rows string
			step("run the saved command",
				chromedp.Poll(`document.querySelector('#run-choice-input')?.value === 'serve'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				clickButton(panel, "Run"),
				chromedp.Poll(`document.querySelector('`+panel+` .terminal .xterm-rows')?.textContent.includes('served from') ?? false`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(`document.querySelector('`+panel+` .terminal .xterm-rows').textContent`, &rows),
			)
			if !strings.Contains(rows, "served from "+filepath.Base(session.WorkingDir)) {
				t.Errorf("run output = %q", rows)
			}
			runs, _ := runner.List(context.Background(), "s1")
			if len(runs) != 1 || runs[0].Name != "serve" || runs[0].Dir != session.WorkingDir {
				t.Fatalf("runs = %+v, want one run of serve in the worktree", runs)
			}
			step("remove the saved command",
				clickButton(panel, "Remove"),
				chromedp.Poll(`document.querySelector('`+panel+`')?.textContent.includes('Removed “serve”.')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				// The field moves to the command that is left.
				chromedp.Poll(`document.querySelector('#run-choice-input')?.value === 'build'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			)
			if got, _ := commands.snapshot(); len(got) != 1 || got["build"] == "" {
				t.Fatalf("after Remove, saved run commands = %v, want only build", got)
			}
		})
	}
}

// failingRunCommands cannot list a repository's run commands.
type failingRunCommands struct{ noRunCommands }

func (failingRunCommands) ListRunCommands(context.Context, string) ([]*domain.RunCommand, error) {
	return nil, &domain.StructuredError{Code: "INTERNAL", Message: "the run commands could not be read"}
}

// When the App tab cannot load the saved commands, or the session has no
// worktree to run in, it says so in a banner instead of leaving Run quietly
// disabled.
func TestAppTabShowsWhyItCannotRun(t *testing.T) {
	for _, c := range []struct {
		name     string
		commands ports.RepositoryRunCommands
		worktree bool
		run      bool
		want     string
	}{
		{"the commands do not load", failingRunCommands{}, true, false, "the run commands could not be read"},
		{"the session has no worktree", noRunCommands{}, false, true, "this session has no worktree to run the application in"},
	} {
		t.Run(c.name, func(t *testing.T) {
			assets, err := NewAssets()
			if err != nil {
				t.Fatal(err)
			}
			if !assets.Built() {
				t.Skip("browser test: web client not built; run `make web`")
			}
			ctx, errs := browser(t)
			session := &domain.Session{
				ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "implement this task",
				Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
				Branch: "agent/add-sse-feed-1a2b",
			}
			if c.worktree {
				session.WorkingDir = t.TempDir()
			}
			sessions := fakeSessions{[]*domain.Session{session}}
			host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
			t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
			commands := c.commands
			runner := apps.NewService(sessions, commands, host)
			pages := NewHandler(Deps{
				Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
				Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
				Agents:       fakeAgents{},
				Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
				Sessions:     sessions,
			}, assets, nil)
			api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{RunCommands: commands, Apps: runner}))
			mux := http.NewServeMux()
			mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler())
			mux.Handle("/api/", api)
			mux.Handle("/", pages)
			srv := httptest.NewServer(withStream(mux))
			defer srv.Close()

			banner := `document.querySelector('[aria-label=Application] [role=alert]')?.textContent ?? ''`
			actions := []chromedp.Action{
				chromedp.EmulateViewport(1280, 800),
				chromedp.Navigate(srv.URL + "/projects/p1/tasks/t1"),
				chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				clickButton(`[role=tablist]`, "App"),
				chromedp.Poll(`!!document.querySelector('#run-choice-input')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			}
			if c.run {
				actions = append(actions,
					chromedp.Click(`#run-choice-input`, chromedp.ByQuery),
					chromedp.KeyEvent("echo hi"),
					chromedp.KeyEvent(kb.Escape),
					clickButton(`[aria-label=Application]`, "Run"),
				)
			}
			var shown string
			actions = append(actions,
				chromedp.Poll(banner+` !== ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Evaluate(banner, &shown),
			)
			if err := chromedp.Run(ctx, actions...); err != nil {
				t.Fatalf("%v\nJS errors: %v", err, errs.all())
			}
			if !strings.Contains(shown, c.want) {
				t.Errorf("banner = %q, want it to say %q", shown, c.want)
			}
		})
	}
}
