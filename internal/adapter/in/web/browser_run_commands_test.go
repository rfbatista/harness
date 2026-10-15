package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
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
)

// memRunCommands keeps run commands in memory under the domain's name rules,
// as the projects service does, and counts the saves that reach it.
type memRunCommands struct {
	mu    sync.Mutex
	byKey map[string]string // repositoryID + "\x00" + name → command
	saves int
}

func (m *memRunCommands) ListRunCommands(_ context.Context, repositoryID string) ([]*domain.RunCommand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*domain.RunCommand{}
	for k, cmd := range m.byKey {
		repo, name, _ := strings.Cut(k, "\x00")
		if repo == repositoryID {
			out = append(out, &domain.RunCommand{RepositoryID: repo, Name: name, Command: cmd, UpdatedAt: time.Now()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memRunCommands) SaveRunCommand(_ context.Context, repositoryID, name, cmd string) (*domain.RunCommand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	name, cmd, err := domain.CleanRunCommand(name, cmd)
	if err != nil {
		return nil, err
	}
	m.byKey[repositoryID+"\x00"+name] = cmd
	return &domain.RunCommand{RepositoryID: repositoryID, Name: name, Command: cmd, UpdatedAt: time.Now()}, nil
}

func (m *memRunCommands) DeleteRunCommand(_ context.Context, repositoryID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := repositoryID + "\x00" + name
	if _, ok := m.byKey[k]; !ok {
		return &domain.StructuredError{Code: "RUN_COMMAND_NOT_FOUND", Message: "no run command " + name}
	}
	delete(m.byKey, k)
	return nil
}

func (m *memRunCommands) snapshot() (map[string]string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.byKey {
		_, name, _ := strings.Cut(k, "\x00")
		out[name] = v
	}
	return out, m.saves
}

// TestRunCommandsInRepositorySettingsInTheBrowser manages a repository's run
// commands from its settings page, against the real HTTP API: the saved ones
// are listed, a refused name is explained inline and saves nothing, an added
// one lists and is offered in a session's App tab, a changed command line
// replaces it under the same name, and Remove deletes one.
func TestRunCommandsInRepositorySettingsInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	store := &memRunCommands{byKey: map[string]string{"r1\x00server": "make air"}}
	session := &domain.Session{
		ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "implement this task",
		Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: time.Now(),
		WorkingDir: t.TempDir(), Branch: "agent/run-1a2b",
	}
	sessions := fakeSessions{[]*domain.Session{session}}
	host := termhost.New(shell.Direct{}, ptyunix.New(), command.Agent{Shell: "/bin/sh"})
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Sessions:     sessions,
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "api", RootDir: "/src/api"}}},
		EnvFiles:     fakeEnv{},
		RunCommands:  store,
	}, assets, nil)
	api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{RunCommands: store, Apps: apps.NewService(sessions, store, host)}))
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler()) // the agent's terminal is not under test
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	const section = `section[aria-labelledby=run-commands-title]`
	pane := func(name string) string { return section + ` section[aria-label="` + name + `"]` }
	settings := srv.URL + "/projects/p1/repositories/r1/env"
	nameProblem := `(document.querySelector('` + section + ` form .error')?.textContent ?? '')`
	addDisabled := `!![...document.querySelectorAll('` + section + ` form button')].find(b => b.textContent.trim() === 'Add command')?.disabled`

	var listed, longProblem, charProblem, offered string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(settings),
		// The saved command is listed with its command line.
		chromedp.Poll(`document.querySelector('`+pane("server")+` input')?.value === 'make air'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('`+section+` section[aria-label]')].map(s => s.getAttribute('aria-label')).join(',')`, &listed),

		// A name of 41 characters is refused inline.
		setField(`#run-command-name`, strings.Repeat("a", 41), "input"),
		setField(`#run-command-line`, "go run ./cmd/worker", "input"),
		chromedp.Poll(`(`+nameProblem+`).length > 0 && `+addDisabled, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(nameProblem, &longProblem),
		// So is a name with a character outside letters, digits, space . _ -.
		setField(`#run-command-name`, "web/app", "input"),
		chromedp.Poll(`(`+nameProblem+`).length > 0 && `+addDisabled, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(nameProblem, &charProblem),
		// Submitting anyway (Enter in the form) saves nothing.
		chromedp.Evaluate(`document.querySelector('`+section+` form').requestSubmit()`, nil),

		// A good name adds the command, and the form empties.
		setField(`#run-command-name`, "worker", "input"),
		chromedp.Poll(`(`+nameProblem+`) === ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(section+` form`, "Add command"),
		chromedp.Poll(`document.querySelector('`+pane("worker")+` input')?.value === 'go run ./cmd/worker'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelector('#run-command-name').value === ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),

		// Editing a command line saves it under the same name.
		setField(pane("server")+` input`, "make run", "input"),
		clickButton(pane("server"), "Save"),
		chromedp.Poll(`(document.querySelector('`+section+` [role=status]')?.textContent ?? '').includes('Saved')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if listed != "server" {
		t.Errorf("listed run commands = %q, want server", listed)
	}
	if !strings.Contains(longProblem, "40") {
		t.Errorf("41-character name problem = %q, want it to name the 40-character limit", longProblem)
	}
	if !strings.Contains(charProblem, ". _ -") {
		t.Errorf("disallowed-character problem = %q, want it to name the allowed characters", charProblem)
	}
	got, _ := store.snapshot()
	if want := map[string]string{"server": "make run", "worker": "go run ./cmd/worker"}; !equalMaps(got, want) {
		t.Errorf("after add and edit, stored = %v, want %v", got, want)
	}
	if _, saves := store.snapshot(); saves != 2 {
		t.Errorf("saves reaching the API = %d, want 2 (refused names save nothing)", saves)
	}

	// The added command is offered in a session's App tab of that repository.
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		clickButton(`[role=tablist]`, "App"),
		chromedp.Poll(`document.querySelectorAll('#run-choice option').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('#run-choice option')].map(o => o.value + ': ' + o.dataset.description).join(', ')`, &offered),
	)
	if err != nil {
		t.Fatalf("App tab: %v\nJS errors: %v", err, errs.all())
	}
	if offered != "server: make run, worker: go run ./cmd/worker" {
		t.Errorf("App tab offers %q", offered)
	}

	// Remove deletes one, after a confirm.
	err = chromedp.Run(ctx,
		chromedp.Navigate(settings),
		chromedp.Poll(`!!document.querySelector('`+pane("worker")+`')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(pane("worker"), "Remove"),
		chromedp.Poll(`[...document.querySelectorAll('`+pane("worker")+` button')].filter(b => b.textContent.trim() === 'Remove').length === 1 && !!document.querySelector('`+pane("worker")+` button[data-variant=danger]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+pane("worker")+` button[data-variant=danger]').click()`, nil),
		chromedp.Poll(`!document.querySelector('`+pane("worker")+`') && !!document.querySelector('`+pane("server")+`')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("remove: %v\nJS errors: %v", err, errs.all())
	}
	if got, _ := store.snapshot(); !equalMaps(got, map[string]string{"server": "make run"}) {
		t.Errorf("after remove, stored = %v", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func equalMaps(a, b map[string]string) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
