package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakePullRequestAPI answers the Web UI contract's pull request routes the
// way the server would, slowly, and pushes the create's echo on the project
// feed before the POST answers.
type fakePullRequestAPI struct {
	mu      sync.Mutex
	posts   []map[string]any
	pr      *domain.PullRequest
	streams []chan string
}

func (f *fakePullRequestAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/pull_request":
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(ports.PullRequestView{Draft: agentDraft(), PullRequest: f.pr})
	case "/api/create_pull_request":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.posts = append(f.posts, body)
		pr := *agentPR
		pr.Title, pr.Draft = fmt.Sprint(body["title"]), body["draft"] == true
		f.pr = &pr
		echo, _ := json.Marshal(map[string]any{"pull_request": pr})
		for _, s := range f.streams {
			s <- string(echo)
		}
		f.mu.Unlock()
		time.Sleep(400 * time.Millisecond) // the push and the host
		_ = json.NewEncoder(w).Encode(map[string]any{"pull_request": pr})
	case "/api/page_events":
		w.Header().Set("Content-Type", "text/event-stream")
		ch := make(chan string, 4)
		f.mu.Lock()
		f.streams = append(f.streams, ch)
		f.mu.Unlock()
		fmt.Fprint(w, ": open\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case msg := <-ch:
				fmt.Fprintf(w, "data: {\"kind\":\"project\",\"change\":%s}\n\n", msg)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	default:
		http.NotFound(w, r)
	}
}

// TestPullRequestPanelInTheBrowser opens a PR from a session's history page:
// the form starts from the draft, a double submit sends one POST, progress
// shows while it runs, and the echo that beats the answer is not taken for
// someone else's PR.
func TestPullRequestPanelInTheBrowser(t *testing.T) {
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
	mux.Handle("/api/", api)
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, History: w.history,
		PullRequests: w.prs, Now: func() time.Time { return now },
	}, assets, nil))
	srv := httptest.NewServer(mux)
	defer func() { srv.CloseClientConnections(); srv.Close() }() // the feed's stream stays open

	shown := func(sel string) string {
		return `(() => { const el = document.querySelector('` + sel + `'); return !!el && getComputedStyle(el).display !== 'none' })()`
	}
	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(5*time.Second))
	}
	var title, progress, chip, notice string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(srv.URL+sessionHistory),
		poll(shown(`main header [x-on\\:click="openForm"]`)),
		chromedp.Evaluate(`document.querySelector('main header [x-on\\:click="openForm"]').click()`, nil),
		poll(shown(`#pr-form`)),
		chromedp.Value(`#pr-title`, &title),
		chromedp.Evaluate(`(() => { const t = document.getElementById('pr-title'); t.value = 'Follow the feed'; t.dispatchEvent(new Event('input')); document.getElementById('pr-draft').click() })()`, nil),
		chromedp.Evaluate(`(() => { const b = document.querySelector('#pr-form button[type=submit]'); b.click(); b.click() })()`, nil),
		poll(shown(`#pr-form [role=status]`)),
		chromedp.Text(`#pr-form [role=status]`, &progress),
		poll(shown(`.pr-chip`)+` && `+`!(`+shown(`#pr-form`)+`)`),
		chromedp.Text(`.pr-chip`, &chip),
		chromedp.Evaluate(`document.querySelector('.pr-strip [x-text=notice]').textContent`, &notice),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if title != "Add SSE feed" {
		t.Errorf("the form starts from the draft's title, got %q", title)
	}
	if progress != "Pushing agent/add-sse-feed-1a2b and opening the pull request on GitHub…" {
		t.Errorf("progress = %q", progress)
	}
	if want := "draft #12 Follow the feed"; normalize(chip) != want {
		t.Errorf("chip = %q, want %q", normalize(chip), want)
	}
	if notice != "" {
		t.Errorf("the person's own PR is not news from elsewhere: %q", notice)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.posts) != 1 || api.posts[0]["title"] != "Follow the feed" || api.posts[0]["draft"] != true || api.posts[0]["workspace_id"] != "ws-agent" {
		t.Errorf("posts = %v: one, with what the form said", api.posts)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// normalize joins an element's text on single spaces.
func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }
