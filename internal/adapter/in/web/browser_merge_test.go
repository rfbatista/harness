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

// fakeMergeAPI answers the Merge tab's offer for each target and records the
// merge it is asked for, as the harness server would (Web UI contract: server
// merge).
type fakeMergeAPI struct {
	mu    sync.Mutex
	posts []map[string]any
	// refuse answers a merge with this coded refusal instead of success.
	refuse map[string]any
}

func (f *fakeMergeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		into := r.URL.Query().Get("target")
		if into == "" {
			into = "main"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available": true, "branch": "agent/add-sse-feed-1a2b", "into": into, "ahead": 3,
			"uncommitted_changes": 0, "suggested_message": "Add SSE feed",
			"targets": []map[string]any{
				{"name": "main", "default": true, "checked_out": true, "dirty": false},
				{"name": "develop", "default": false, "checked_out": false, "dirty": false},
				{"name": "release", "default": false, "checked_out": false, "dirty": false},
			},
		})
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.posts = append(f.posts, body)
	refuse := f.refuse
	f.mu.Unlock()
	if refuse != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(refuse)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"at": time.Now().Format(time.RFC3339), "merged": true, "method": body["method"], "into": body["target"], "head": "0123456789abcdef",
	})
}

func mergeTabServer(t *testing.T, api http.Handler) string {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	s := liveSession()
	s.Status, s.Interactive, s.Branch, s.WorkspaceID = domain.SessionDone, true, "agent/add-sse-feed-1a2b", "ws-1"
	pages := NewHandler(Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:        fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:     fakeSessions{[]*domain.Session{s}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/sessions/s1/merge", api)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

const mergeText = `(sel) => document.querySelector(sel).textContent.replace(/\s+/g, ' ').trim()`

// A squash into a branch that is not the default: the person picks the target
// and the method, reads what will happen, writes the message, and sees the
// result with the new HEAD and that nothing else changed.
func TestMergeTabSquashesIntoAnotherTarget(t *testing.T) {
	api := &fakeMergeAPI{}
	url := mergeTabServer(t, api)
	ctx, errs := browser(t)

	var before, after, button, result, role string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t1"),
		clickButton(`.pane-tabs`, "Merge"),
		chromedp.Poll(`!!document.querySelector('[data-merge]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(`+mergeText+`)('[data-merge-consequence]')`, &before),
		// Into: the select behind the picker is the source of truth.
		chromedp.Evaluate(`(() => { const s = document.getElementById('merge-into'); s.value = 'develop'; s.dispatchEvent(new Event('change', { bubbles: true })) })()`, nil),
		chromedp.Poll(`document.querySelector('[data-merge]').textContent.trim() === 'Merge into develop'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`input[name=merge-method][value=squash]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.getElementById('merge-message') && document.getElementById('merge-message').value === 'Add SSE feed'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(`+mergeText+`)('[data-merge-consequence]')`, &after),
		chromedp.Evaluate(`document.querySelector('[data-merge]').textContent.trim()`, &button),
		chromedp.Click(`[data-merge]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('[data-merge-result]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(`+mergeText+`)('[data-merge-result]')`, &result),
		chromedp.Evaluate(`document.querySelector('[data-merge-result]').getAttribute('role')`, &role),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(before, "Merges 3 commits from agent/add-sse-feed-1a2b into main with a merge commit. main is checked out in the main checkout") {
		t.Errorf("what will happen, before = %q", before)
	}
	if !strings.Contains(after, "Squashes 3 commits from agent/add-sse-feed-1a2b into one new commit on develop") || !strings.Contains(after, "The main checkout is not touched. Nothing is pushed.") {
		t.Errorf("what will happen, after = %q", after)
	}
	if button != "Squash into develop" {
		t.Errorf("button = %q", button)
	}
	for _, want := range []string{"Merged into develop", "develop is now at 01234567", "Nothing was pushed. The session, its worktree and the task status are unchanged."} {
		if !strings.Contains(result, want) {
			t.Errorf("result %q lacks %q", result, want)
		}
	}
	if role != "status" {
		t.Errorf("a success is a status, got role %q", role)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.posts) != 1 || api.posts[0]["target"] != "develop" || api.posts[0]["method"] != "squash" || api.posts[0]["message"] != "Add SSE feed" {
		t.Errorf("merge requests = %v", api.posts)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A conflict names the files and sends the person to the agent; it is an alert.
func TestMergeTabNamesTheConflictingFiles(t *testing.T) {
	api := &fakeMergeAPI{refuse: map[string]any{
		"error": "MERGE_CONFLICT: the merge conflicts", "code": "MERGE_CONFLICT", "details": map[string]any{"files": []string{"a.go", "b/c.go"}},
	}}
	url := mergeTabServer(t, api)
	ctx, errs := browser(t)

	var result, role string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/projects/p1/tasks/t1"),
		clickButton(`.pane-tabs`, "Merge"),
		chromedp.Poll(`!!document.querySelector('[data-merge]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`[data-merge]`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('[data-merge-result]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(`+mergeText+`)('[data-merge-result]')`, &result),
		chromedp.Evaluate(`document.querySelector('[data-merge-result]').getAttribute('role')`, &role),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(result, "Conflicts in a.go, b/c.go.") || !strings.Contains(result, "Ask the agent to merge main into its branch") || !strings.Contains(result, "MERGE_CONFLICT") {
		t.Errorf("result = %q", result)
	}
	if role != "alert" {
		t.Errorf("a refusal is an alert, got role %q", role)
	}
}
