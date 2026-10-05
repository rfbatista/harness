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
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rows := `document.querySelector('[aria-label=Application] .terminal .xterm-rows')?.textContent ?? ''`
	// The ANSI color reaches xterm as a style, not as text.
	colored := `[...document.querySelectorAll('[aria-label=Application] .xterm-rows span')].some(s => s.textContent.includes('app is up') && [...s.classList].some(c => c.startsWith('xterm-fg-')))`
	var runState, afterStop, source, worktree string
	var shot []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		clickButton(`[role=tablist]`, "App"),
		chromedp.Poll(`!!document.querySelector('[aria-label="Command to run"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		setField(`[aria-label="Command to run"]`, `printf '\033[32mapp is up\033[0m in %s\n' "$(basename "$PWD")"; sleep 30`, "input"),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .source p').textContent.replace(/\s+/g, ' ').trim()`, &source),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .source .code').textContent`, &worktree),
		clickButton(`[aria-label=Application]`, "Run"),
		chromedp.Poll(colored, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.FullScreenshot(&shot, 80),
		clickButton(`[aria-label=Application]`, "Stop"),
		chromedp.Poll(`document.querySelector('[aria-label=Application] .repel .status')?.textContent === 'stopped'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[aria-label=Application] .repel .status')?.textContent ?? ''`, &runState),
		// The terminal reports the exit; the output stays on screen.
		chromedp.Poll(`document.querySelector('[aria-label=Application] .terminal .bar .status')?.textContent.startsWith('exited')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(rows, &afterStop),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
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
