package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// fakeStartAPI lists branches and records the start request.
type fakeStartAPI struct {
	mu      sync.Mutex
	started map[string]any
}

func (f *fakeStartAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/list_branches":
		_ = json.NewEncoder(w).Encode(map[string]any{"branches": []map[string]any{
			{"name": "main", "remote": false, "is_head": true},
			{"name": "feat/feed", "remote": false, "is_head": false},
			{"name": "origin/release", "remote": true, "is_head": false},
		}})
	case "/api/start_interactive_session":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.started = in
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{
			"id": "s-new", "project_id": "p1", "ticket_id": "t1", "task": "", "status": "running",
			"interactive": true, "runs_on": "server", "updated_at": time.Now().Format(time.RFC3339),
		}})
	default:
		http.NotFound(w, r)
	}
}

func TestNewSessionBranchesOffTheChosenBranch(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeStartAPI{}
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/src/harness"}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var options, filtered []string
	var preselected, shown string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		clickButton(`main header`, "New session"),
		chromedp.Poll(`document.querySelectorAll('#new-session-base option').length === 4`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelector('#new-session-base').value === 'main'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-base option')].map(o => (o.parentElement.label || '') + ':' + o.textContent)`, &options),
		chromedp.Evaluate(`document.querySelector('#new-session-base').value`, &preselected),
		// The Branch off combobox: type to filter, the checked-out branch stays pinned.
		chromedp.Evaluate(`document.querySelector('#new-session-base-input').focus()`, nil),
		chromedp.SendKeys(`#new-session-base-input`, "rel", chromedp.ByQuery),
		chromedp.Poll(`(document.querySelector('#new-session-base-input').getAttribute('aria-expanded') === 'true')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-base-listbox [role=option] .label')].map(l => l.textContent)`, &filtered),
		pressEnter(),
		chromedp.Poll(`document.querySelector('#new-session-base').value === 'origin/release'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('#new-session-base-input').value`, &shown),
		// Permissions is a radio group: all three choices in sight.
		chromedp.Evaluate(`document.querySelector('#new-session-permissions-all').click()`, nil),
		clickButton(`main form`, "Start session"),
		chromedp.Poll(`!document.querySelector('form[x-data^=sessionsNewSession]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	want := ":The checked-out branch,Branches:main,Branches:feat/feed,Remote branches:origin/release"
	if got := strings.Join(options, ","); got != want {
		t.Errorf("options = %s\nwant      %s", got, want)
	}
	if got := strings.Join(filtered, ","); got != "The checked-out branch,origin/release" {
		t.Errorf("filtered on \"rel\" = %s, want the pinned row and origin/release", got)
	}
	if shown != "origin/release" {
		t.Errorf("the field shows %q after Enter, want origin/release", shown)
	}
	if preselected != "main" {
		t.Errorf("preselected %q, want the checked-out main", preselected)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.started["base_branch"] != "origin/release" || api.started["repository_id"] != "r1" || api.started["auto_accept"] != "all" {
		t.Errorf("start request = %v", api.started)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
