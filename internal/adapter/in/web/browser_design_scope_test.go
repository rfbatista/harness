package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// scopedArtifactsAPI stands in for the server's artifact routes with scopes,
// in the shape of Contract: Harness server ↔ Web UI — artifact scope API:
// the list (by session, or by project and scope), set_artifact_scope, the
// delete, the view route and the session's event stream. A move is pushed on
// the stream before its answer, the order the browser must survive.
type scopedArtifactsAPI struct {
	mu     sync.Mutex
	list   []map[string]any // newest first
	feed   *sseFeed
	refuse string // a code set_artifact_scope answers with, when not ""
	moves  []string
}

func scopedDTO(id, kind, title, path string, scope string, at time.Time) map[string]any {
	a := artifactDTO(id, kind, title, "", path, 1, at)
	a["scope"] = scope
	if kind == "url" {
		a["path"], a["url"], a["mime"], a["size_bytes"] = nil, "http://localhost:5173/", "", 0
	}
	return a
}

func (a *scopedArtifactsAPI) find(id string) map[string]any {
	for _, x := range a.list {
		if x["id"] == id {
			return x
		}
	}
	return nil
}

func (a *scopedArtifactsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/sessions/s1/events" && a.feed != nil {
		a.feed.ServeHTTP(w, r)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/artifacts":
		out := []map[string]any{}
		for _, x := range a.list {
			bySession := q.Get("session_id") != "" && x["session_id"] == q.Get("session_id")
			byProject := q.Get("project_id") != "" && x["project_id"] == q.Get("project_id")
			if (bySession || byProject) && (q.Get("scope") == "" || x["scope"] == q.Get("scope")) {
				out = append(out, x)
			}
		}
		writeJSON(http.StatusOK, map[string]any{"artifacts": out})
	case r.Method == http.MethodPost && r.URL.Path == "/api/set_artifact_scope":
		var in struct {
			ArtifactID string `json:"artifact_id"`
			Scope      string `json:"scope"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		a.moves = append(a.moves, in.ArtifactID+"→"+in.Scope)
		if a.refuse != "" {
			writeJSON(http.StatusConflict, map[string]any{"error": "the artifact's session and worktree are gone, so there are no bytes to keep", "code": a.refuse})
			return
		}
		x := a.find(in.ArtifactID)
		if x == nil {
			writeJSON(http.StatusNotFound, map[string]any{"error": "artifact not found", "code": "ARTIFACT_NOT_FOUND"})
			return
		}
		if x["scope"] != in.Scope {
			at, _ := time.Parse(time.RFC3339, x["updated_at"].(string))
			x["scope"], x["updated_at"] = in.Scope, at.Add(time.Second).Format(time.RFC3339)
			if a.feed != nil {
				a.feed.send(map[string]any{"seq": 100, "session_id": x["session_id"], "type": "artifact", "artifact": x, "at": x["updated_at"]})
			}
		}
		writeJSON(http.StatusOK, map[string]any{"artifact": x})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/artifacts/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
		i := slices.IndexFunc(a.list, func(x map[string]any) bool { return x["id"] == id })
		if i < 0 {
			writeJSON(http.StatusNotFound, map[string]any{"error": "artifact not found", "code": "ARTIFACT_NOT_FOUND"})
			return
		}
		a.list = slices.Delete(a.list, i, i+1)
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/view/"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'self' data: blob:; frame-ancestors 'self'")
		fmt.Fprint(w, "<!doctype html><title>artifact</title><p>asset</p>")
	default:
		http.NotFound(w, r)
	}
}

// send pushes v to every follower from a handler, where there is no *testing.T.
func (f *sseFeed) send(v any) {
	b, _ := json.Marshal(v)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c <- b
	}
}

// designServer serves the task page of t1 (one live design session, s1) and
// the project's pages over the scoped artifacts stand-in.
func designServer(t *testing.T, api *scopedArtifactsAPI) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	now := time.Now()
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t1", ProjectID: "p1", Title: "Pricing page", Status: domain.TicketStatusInProgress},
			{ID: "t2", ProjectID: "p1", Title: "Brand refresh", Status: domain.TicketStatusTodo},
		}},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions: fakeSessions{[]*domain.Session{{
			ID: "s1", ProjectID: "p1", TicketID: "t1", RepositoryID: "r1", Task: "design the pricing page", Mode: domain.SessionMode("design"),
			Status: domain.SessionIdle, Interactive: true, RunsOn: domain.RunnerServer, UpdatedAt: now,
		}}},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/events", http.NotFoundHandler())
	mux.Handle("/api/sessions/s1/terminal", http.NotFoundHandler())
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

const (
	designPanelSel = `document.querySelector('[aria-label="Design"]')`
	announcer      = `document.querySelector('[role=status][aria-live=polite]').textContent`
)

func cardNamed(title string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('[aria-label="Design"] .artifact-card')].find(c => c.querySelector('.title').textContent === %q)`, title)
}

// TestDesignTabMovesAnArtifactToTheProjectInTheBrowser: the bar says the
// selected artifact's scope and moves it; the card is marked in place. The
// person's own move, echoed on the stream before its answer, neither flashes
// the card nor counts on the tab. A dev server has no move to the project.
// An agent's move shows live, announced but not counted. A refusal reads as
// the server put it.
func TestDesignTabMovesAnArtifactToTheProjectInTheBrowser(t *testing.T) {
	ctx, errs := browser(t)
	now := time.Now()
	api := &scopedArtifactsAPI{feed: &sseFeed{}, list: []map[string]any{
		scopedDTO("a1", "page", "Hero", "design/hero.html", "task", now.Add(-time.Minute)),
		scopedDTO("u1", "url", "Dev server", "", "task", now.Add(-2*time.Minute)),
	}}
	srv := designServer(t, api)
	designTab := `[...document.querySelectorAll('[role=tab]')].find(b => b.textContent.trim().startsWith('Design'))`
	hero := cardNamed("Hero")
	moveButton := designPanelSel + `.querySelector('.bar form button')`

	var scopeBefore, label, aria, scopeAfter, labelAfter, mark, announced string
	var fresh, badge bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('[role=tab]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		clickButton(`[role=tablist]`, "Design"),
		chromedp.Poll(`!!(`+moveButton+`)`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(designPanelSel+`.querySelector('[data-scope-word]').textContent`, &scopeBefore),
		chromedp.Evaluate(moveButton+`.textContent`, &label),
		chromedp.Evaluate(moveButton+`.getAttribute('aria-label')`, &aria),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	waitFor(t, "the session stream", func() bool { return api.feed.followers() >= 1 })
	err = chromedp.Run(ctx,
		chromedp.Evaluate(moveButton+`.click(); true`, nil),
		chromedp.Poll(designPanelSel+`.querySelector('[data-scope-word]').textContent === 'Project asset'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(200*time.Millisecond), // let the echo land
		chromedp.Evaluate(designPanelSel+`.querySelector('[data-scope-word]').textContent`, &scopeAfter),
		chromedp.Evaluate(moveButton+`.textContent`, &labelAfter),
		chromedp.Evaluate(`(`+hero+`).querySelector('.badge[data-tone=signal]')?.textContent ?? ''`, &mark),
		chromedp.Evaluate(`(`+hero+`).hasAttribute('data-fresh')`, &fresh),
		chromedp.Evaluate(designTab+`.querySelector('.badge') !== null`, &badge),
		chromedp.Evaluate(announcer, &announced),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if scopeBefore != "Task asset" || label != "Move to project" || aria != "Move Hero to project" {
		t.Errorf("before: scope %q, label %q, aria %q", scopeBefore, label, aria)
	}
	if scopeAfter != "Project asset" || labelAfter != "Move back to task" || mark != "project" {
		t.Errorf("after: scope %q, label %q, card mark %q", scopeAfter, labelAfter, mark)
	}
	if fresh || badge || strings.HasPrefix(announced, "Moved") {
		t.Errorf("the person's own move read as news: fresh %v, tab badge %v, announced %q", fresh, badge, announced)
	}
	if got := fmt.Sprint(api.moves); got != "[a1→project]" {
		t.Errorf("set_artifact_scope calls = %s", got)
	}

	// A dev server has nothing to keep: no move to the project.
	var urlMove bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`(`+cardNamed("Dev server")+`).click(); true`, nil),
		chromedp.Poll(designPanelSel+`.querySelector('[data-scope-word]')?.textContent === 'Task asset'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!!(`+moveButton+`)`, &urlMove),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if urlMove {
		t.Error("a dev-server url offers a move")
	}

	// An agent moves Hero back behind the Agent tab: the card re-badges
	// live, the move is announced and not counted.
	if err := chromedp.Run(ctx, clickButton(`[role=tablist]`, "Agent")); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	moved := scopedDTO("a1", "page", "Hero", "design/hero.html", "task", now.Add(time.Hour))
	api.list[0] = moved
	api.mu.Unlock()
	api.feed.pushRaw(t, map[string]any{"seq": 101, "session_id": "s1", "type": "artifact", "artifact": moved, "at": moved["updated_at"]})
	err = chromedp.Run(ctx,
		chromedp.Poll(announcer+` === 'Moved back to task: Hero'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(`+hero+`).querySelector('.badge[data-tone=signal]')?.textContent ?? ''`, &mark),
		chromedp.Evaluate(designTab+`.querySelector('.badge') !== null`, &badge),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if mark != "" || badge {
		t.Errorf("after the agent's move: card mark %q, tab badge %v", mark, badge)
	}

	// The server refuses: the banner says why and what to do.
	api.mu.Lock()
	api.refuse = "ARTIFACT_NOT_PROMOTABLE"
	api.mu.Unlock()
	var banner string
	err = chromedp.Run(ctx,
		clickButton(`[role=tablist]`, "Design"),
		chromedp.Evaluate(`(`+hero+`).click(); true`, nil),
		chromedp.Poll(`!!(`+moveButton+`)`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(moveButton+`.click(); true`, nil),
		chromedp.Poll(`!!`+designPanelSel+`.querySelector('[role=alert]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(designPanelSel+`.querySelector('[role=alert]').textContent`, &banner),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "worktree are gone") || !strings.Contains(banner, "ARTIFACT_NOT_PROMOTABLE") {
		t.Errorf("refusal banner = %q", banner)
	}
}

// TestProjectDesignLibraryInTheBrowser: the rail opens the project's design
// assets; they list without the task-scoped one, each naming its task, the
// newest framed in a sandbox, titles as text. Moved back, an asset leaves the
// list and the banner links its task; deleted after asking, it is gone.
func TestProjectDesignLibraryInTheBrowser(t *testing.T) {
	ctx, errs := browser(t)
	now := time.Now()
	hostile := `<img src=x onerror="document.title='pwned'">`
	logo := scopedDTO("a-logo", "page", "Logo", "brand/logo.html", "project", now)
	logo["ticket_id"] = "t2"
	api := &scopedArtifactsAPI{list: []map[string]any{
		logo,
		scopedDTO("a-card", "page", hostile, "design/card.html", "project", now.Add(-time.Minute)),
		scopedDTO("a-draft", "page", "Draft", "design/draft.html", "task", now.Add(-2*time.Minute)),
	}}
	srv := designServer(t, api)
	main := `document.querySelector('main[x-data="sessionsDesignLibrary"]')`
	titles := `[...` + main + `.querySelectorAll('.artifact-card .title')].map(e => e.textContent)`

	var path, sandbox, src, from, count, pageTitle string
	var shot []byte
	var cards []string
	var imgInCard bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(`!!document.querySelector('nav[aria-label="Tasks"] a[href="/projects/p1/design"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('nav[aria-label="Tasks"] a[href="/projects/p1/design"]').click()`, nil),
		waitForPath(srv.URL+"/projects/p1/design", &path),
		chromedp.Poll(main+`.querySelectorAll('.artifact-card').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(titles, &cards),
		chromedp.Evaluate(main+`.querySelector('.artifact-card [data-task]').textContent.trim()`, &from),
		chromedp.Evaluate(main+`.querySelector('.artifact-card img') !== null`, &imgInCard),
		chromedp.Poll(main+`.querySelector('.preview iframe')?.getAttribute('src')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(main+`.querySelector('.preview iframe').getAttribute('sandbox')`, &sandbox),
		chromedp.Evaluate(main+`.querySelector('.preview iframe').getAttribute('src')`, &src),
		chromedp.Evaluate(`document.querySelector('header [x-text="countWord"]').textContent`, &count),
		chromedp.FullScreenshot(&shot, 80),
		chromedp.Evaluate(`document.title`, &pageTitle),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if got := fmt.Sprint(cards); got != fmt.Sprint([]string{"Logo", hostile}) || imgInCard {
		t.Errorf("cards = %s (img in card: %v), want the project's two, newest first, as text", got, imgInCard)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/project-design.jpg", shot, 0o644)
	}
	if !strings.HasPrefix(pageTitle, "Design assets") {
		t.Errorf("page title = %q: a title ran as markup?", pageTitle)
	}
	if from != "from Brand refresh" || sandbox != "allow-scripts" || src != "/api/artifacts/a-logo/view/?rev=1" || count != "2 assets" {
		t.Errorf("from %q, sandbox %q, src %q, count %q", from, sandbox, src, count)
	}

	// Move the logo back to its task: it leaves, the banner links the task.
	var href, linkText string
	err = chromedp.Run(ctx,
		clickButton(`main .preview .bar`, "Move back to task"),
		chromedp.Poll(`!!document.querySelector('[data-moved-back] a')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-moved-back] a').getAttribute('href')`, &href),
		chromedp.Evaluate(`document.querySelector('[data-moved-back] a').textContent`, &linkText),
		chromedp.Evaluate(titles, &cards),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if href != "/projects/p1/tasks/t2" || linkText != "Brand refresh" || len(cards) != 1 {
		t.Errorf("after the move back: link %q %q, cards %v", href, linkText, cards)
	}

	// Delete asks first, then the last asset is gone and the page says how they arrive.
	var question string
	var empty bool
	err = chromedp.Run(ctx,
		clickButton(`main .preview .bar`, "Delete"),
		chromedp.Poll(`!!document.querySelector('[role=alertdialog]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=alertdialog] strong').textContent`, &question),
		clickButton(`[role=alertdialog]`, "Delete asset"),
		chromedp.Poll(`document.body.textContent.includes('No design asset has been moved to this project yet')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!document.querySelector('[role=alertdialog]')`, &empty),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if question != "Delete "+hostile+"?" || !empty {
		t.Errorf("question %q, dialog closed %v", question, empty)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := fmt.Sprint(api.moves); got != "[a-logo→task]" {
		t.Errorf("set_artifact_scope calls = %s", got)
	}
	if len(api.list) != 2 || api.find("a-card") != nil {
		t.Errorf("after delete the stand-in holds %d artifacts, a-card still there: %v", len(api.list), api.find("a-card") != nil)
	}
}
