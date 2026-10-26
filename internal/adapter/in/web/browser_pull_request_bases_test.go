package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/ports"
)

// basesAPI adds the pull request base routes to the fake PR API: origin has
// main and develop, and a refresh fails the way a lost network does.
type basesAPI struct{ *fakePullRequestAPI }

func (f basesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/pull_request_bases":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"workspace_id": "ws-agent", "branches": []string{"develop", "main"}, "default": "main", "fetched_at": time.Now().Add(-3 * time.Minute).Format(time.RFC3339)})
	case "/api/refresh_pull_request_bases":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "PR_FETCH_FAILED: Could not resolve host: github.com", "code": "PR_FETCH_FAILED"})
	default:
		f.fakePullRequestAPI.ServeHTTP(w, r)
	}
}

// The PR form's Base is a picker of origin's branches; a failed Refresh says
// why in one line and keeps the list; Open as draft changes the button and
// the create sends the picked base and draft.
func TestPullRequestFormPicksTheBaseFromOriginsBranches(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	w := historyWorld(t)
	w.prs = fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft()}}}
	api := &fakePullRequestAPI{}
	mux := http.NewServeMux()
	mux.Handle("/api/", basesAPI{api})
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		PullRequests: w.prs, Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	defer func() { srv.CloseClientConnections(); srv.Close() }()

	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(5*time.Second))
	}
	var options []string
	var fetched, refreshError, consequence, submit string
	var optionsAfter []string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+sessionHistory),
		poll(`!!document.querySelector('main header [x-on\\:click="openForm"]')`),
		chromedp.Evaluate(`document.querySelector('main header [x-on\\:click="openForm"]').click()`, nil),
		poll(`document.querySelectorAll('#pr-base option').length === 2`),
		chromedp.Evaluate(`[...document.querySelectorAll('#pr-base option')].map(o => o.value)`, &options),
		chromedp.Evaluate(`document.querySelector('[data-fetched]').textContent`, &fetched),
		chromedp.Click(`[data-refresh-bases]`, chromedp.ByQuery),
		poll(`document.querySelector('[data-refresh-error]') && getComputedStyle(document.querySelector('[data-refresh-error]')).display !== 'none'`),
		chromedp.Evaluate(`document.querySelector('[data-refresh-error]').textContent`, &refreshError),
		chromedp.Evaluate(`[...document.querySelectorAll('#pr-base option')].map(o => o.value)`, &optionsAfter),
		chromedp.Evaluate(`(() => { const s = document.getElementById('pr-base'); s.value = 'develop'; s.dispatchEvent(new Event('change', { bubbles: true })); document.getElementById('pr-draft').click() })()`, nil),
		poll(`document.querySelector('#pr-form button[type=submit]').textContent.trim() === 'Open draft PR'`),
		chromedp.Evaluate(`document.querySelector('[data-pr-consequence]').textContent`, &consequence),
		chromedp.Evaluate(`document.querySelector('#pr-form button[type=submit]').textContent.trim()`, &submit),
		chromedp.Evaluate(`(() => { const t = document.getElementById('pr-title'); t.value = 'Follow the feed'; t.dispatchEvent(new Event('input')); document.querySelector('#pr-form button[type=submit]').click() })()`, nil),
		poll(`!!document.querySelector('.pr-chip') && getComputedStyle(document.querySelector('.pr-chip')).display !== 'none' && !!document.querySelector('[data-pr-created-note]') && getComputedStyle(document.querySelector('[data-pr-created-note]')).display !== 'none'`),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if got := strings.Join(options, ","); got != "main,develop" {
		t.Errorf("base options = %s, want the default first, then origin's", got)
	}
	if !strings.Contains(fetched, "Last fetched 3 min ago") {
		t.Errorf("fetched line = %q", fetched)
	}
	if !strings.Contains(refreshError, "Could not fetch origin.") || !strings.Contains(refreshError, "Could not resolve host: github.com") {
		t.Errorf("refresh error = %q", refreshError)
	}
	if got := strings.Join(optionsAfter, ","); got != "main,develop" {
		t.Errorf("the list after a failed refresh = %s, want it kept", got)
	}
	if want := "Pushes agent/add-sse-feed-1a2b to origin and opens a draft pull request into develop."; normalize(consequence) != want {
		t.Errorf("consequence = %q, want %q", normalize(consequence), want)
	}
	if submit != "Open draft PR" {
		t.Errorf("button = %q", submit)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.posts) != 1 || api.posts[0]["base_branch"] != "develop" || api.posts[0]["draft"] != true {
		t.Errorf("posts = %v: one, on develop, as a draft", api.posts)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
