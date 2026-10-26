package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// fakeBranchNameAPI answers the branch name check and the start the way the
// harness does: feature/login exists, and a start on it is refused. race/name
// is reported free by the check and refused by the start, as a name taken in
// between.
type fakeBranchNameAPI struct {
	mu      sync.Mutex
	started map[string]any
	checks  []string
}

func (f *fakeBranchNameAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/list_branches":
		_ = json.NewEncoder(w).Encode(map[string]any{"branches": []map[string]any{{"name": "main", "is_head": true}}})
	case "/api/check_branch_name":
		name := r.URL.Query().Get("name")
		f.mu.Lock()
		f.checks = append(f.checks, name)
		f.mu.Unlock()
		out := map[string]any{"name": name, "generated": "agent/add-sse-feed-1a2b", "valid": true, "available": true, "code": "", "reason": "", "suggestion": ""}
		if name == "feature/login" {
			out["available"], out["code"], out["reason"], out["suggestion"] = false, "BRANCH_EXISTS", "feature/login already exists", "feature/login-2"
		}
		_ = json.NewEncoder(w).Encode(out)
	case "/api/start_interactive_session":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if name, _ := in["branch_name"].(string); name == "feature/login" || name == "race/name" {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "BRANCH_EXISTS: " + name + " already exists", "code": "BRANCH_EXISTS", "details": map[string]any{"name": name, "suggestion": name + "-2"}})
			return
		}
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

func newBranchNameServer(t *testing.T, api http.Handler) string {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
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
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNewSessionNamesItsBranchAndOffersTheFreeNameWhenTaken(t *testing.T) {
	ctx, errs := browser(t)
	api := &fakeBranchNameAPI{}
	url := newBranchNameServer(t, api)

	var prefilled, taken, free string
	var startDisabled bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t1"),
		clickButton(`main header`, "New session"),
		chromedp.Poll(`document.querySelector('#new-session-branch-name')?.value === 'agent/add-sse-feed-1a2b'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('#new-session-branch-name').value`, &prefilled),
		// Focus selects the prefilled name, so typing replaces it.
		chromedp.Focus(`#new-session-branch-name`, chromedp.ByQuery),
		chromedp.SendKeys(`#new-session-branch-name`, "feature/login", chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('[data-branch-name-suggestion]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-branch-name-status]').textContent.replace(/\s+/g, ' ').trim()`, &taken),
		chromedp.Evaluate(`document.querySelector('[type=submit]').disabled && document.querySelector('[data-start-reason]').textContent.length > 0`, &startDisabled),
		chromedp.Click(`[data-branch-name-suggestion]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-branch-name-status]').dataset.state === 'free'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('#new-session-branch-name').value`, &free),
		clickButton(`main form`, "Start session"),
		chromedp.Poll(`!document.querySelector('form[x-data^=sessionsNewSession]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if prefilled != "agent/add-sse-feed-1a2b" {
		t.Errorf("prefilled %q", prefilled)
	}
	if !strings.Contains(taken, "feature/login already exists on this repository") || !strings.Contains(taken, "Use feature/login-2") {
		t.Errorf("status of a taken name = %q", taken)
	}
	if !startDisabled {
		t.Error("Start session stays enabled, or gives no reason, while the name is taken")
	}
	if free != "feature/login-2" {
		t.Errorf("the chip filled %q", free)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.started["branch_name"] != "feature/login-2" {
		t.Errorf("start request = %v, want branch_name feature/login-2", api.started)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A name taken between the check and the start is refused by the server; the
// form shows it on the field with the same words and keeps what was typed.
func TestNewSessionShowsAStartRefusedForItsNameOnTheField(t *testing.T) {
	ctx, errs := browser(t)
	api := &fakeBranchNameAPI{}
	url := newBranchNameServer(t, api)

	var status, prompt string
	var banners int
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t1"),
		clickButton(`main header`, "New session"),
		chromedp.Poll(`document.querySelector('#new-session-branch-name')?.value === 'agent/add-sse-feed-1a2b'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys(`#new-session-prompt`, "go on", chromedp.ByQuery),
		chromedp.Focus(`#new-session-branch-name`, chromedp.ByQuery),
		chromedp.SendKeys(`#new-session-branch-name`, "race/name", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-branch-name-status]').dataset.state === 'free'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		clickButton(`main form`, "Start session"),
		chromedp.Poll(`document.querySelector('[data-branch-name-status]').dataset.state === 'taken'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-branch-name-status]').textContent.replace(/\s+/g, ' ').trim()`, &status),
		chromedp.Evaluate(`document.querySelector('#new-session-prompt').value`, &prompt),
		chromedp.Evaluate(`document.querySelectorAll('form[x-data^=sessionsNewSession] [role=alert]').length`, &banners),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(status, "race/name already exists on this repository") || !strings.Contains(status, "Use race/name-2") {
		t.Errorf("status after a refused start = %q", status)
	}
	if prompt != "go on" {
		t.Errorf("the prompt became %q", prompt)
	}
	if banners != 0 {
		t.Errorf("%d error banners, want the field alone to say it", banners)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
