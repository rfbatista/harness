package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// codeWorld is a project whose one repository is the code fixture (a git
// repository with a session's worktree and a gone one), read through the
// real code browser, in process for the pages and over /api for the browser.
type codeWorld struct {
	f      *portstest.CodeFixture
	at     portstest.CodeTarget
	code   ports.CodeBrowser
	srv    *httptest.Server
	repoID string
	feed   *fakeFeed
}

// fakeFeed is the project feed (/api/events) the test speaks for: every
// connected page receives what send writes.
type fakeFeed struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

func newFakeFeed() *fakeFeed { return &fakeFeed{clients: map[chan string]bool{}} }

func (f *fakeFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	ch := make(chan string, 16)
	f.mu.Lock()
	f.clients[ch] = true
	f.mu.Unlock()
	defer func() { f.mu.Lock(); delete(f.clients, ch); f.mu.Unlock() }()
	_, _ = w.Write([]byte(": hello\n\n"))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = w.Write([]byte("data: " + msg + "\n\n"))
			flusher.Flush()
		}
	}
}

// connected waits until a page follows the feed, so what is sent next is heard.
func (f *fakeFeed) connected() chromedp.Action {
	return chromedp.ActionFunc(func(context.Context) error {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			f.mu.Lock()
			n := len(f.clients)
			f.mu.Unlock()
			if n > 0 {
				return nil
			}
		}
		return errors.New("no page follows the feed")
	})
}

// send writes one data line to every page following the feed.
func (f *fakeFeed) send(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.clients {
		ch <- msg
	}
}

func newCodeWorld(t *testing.T, deps func(w *codeWorld, d *Deps)) *codeWorld {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := &codeWorld{f: portstest.NewCodeFixture(t)}
	code, at := workspacestest.NewCode(t, w.f)
	w.code, w.at, w.repoID = code, at, at.RepositoryID
	d := Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: w.f.ProjectID, Name: "fixture"}}},
		Tasks:        fakeTickets{},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: at.RepositoryID, ProjectID: w.f.ProjectID, Name: "fixture", RootDir: w.f.RepoDir}}},
		Code:         code,
	}
	if deps != nil {
		deps(w, &d)
	}
	w.feed = newFakeFeed()
	mux := http.NewServeMux()
	mux.Handle("/api/events", w.feed)
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Code: code})))
	mux.Handle("/", NewHandler(d, assets, nil))
	w.srv = httptest.NewServer(mux)
	t.Cleanup(w.srv.Close)
	return w
}

func saveShot(name string, shot []byte) {
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, name), shot, 0o644)
	}
}

// TestRepositoryCodePageInTheBrowser opens a repository's code page: the
// tree painted by the server, a file opened from it with its lines and a
// line targeted from the URL.
func TestRepositoryCodePageInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, nil)
	ctx, errs := browser(t)
	base := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code"

	var ssrRows, rows int
	var names []string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('.file-tree [role=treeitem]').length`, &rows),
		chromedp.Evaluate(`[...document.querySelectorAll('.file-tree [role=treeitem] .name')].map(n => n.textContent.trim())`, &names),
		chromedp.Click(`.file-tree [title="internal"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('.file-tree [title="internal/app"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`.file-tree [title="internal/app"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('.file-tree [title="internal/app/app.go"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`.file-tree [title="internal/app/app.go"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('.code-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-repository.jpg", shot)
	_ = ssrRows
	if rows == 0 || len(names) == 0 {
		t.Fatalf("tree rows = %d %v", rows, names)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	t.Logf("tree: %v", names)
}

var _ = context.Background

// fakeWorkspaces names the fixture worktrees' base branch.
type fakeWorkspaces struct{ base string }

func (f fakeWorkspaces) Get(id string) *domain.Workspace {
	return &domain.Workspace{ID: id, BaseRef: f.base}
}
func (fakeWorkspaces) ListBranches(string) ([]domain.GitBranch, error) {
	return []domain.GitBranch{{Name: "main", IsHead: true}}, nil
}

// withSession adds a task and its running session on the fixture's worktree.
func withSession(status domain.SessionStatus, workspace func(w *codeWorld) string) func(w *codeWorld, d *Deps) {
	return func(w *codeWorld, d *Deps) {
		d.Tasks = fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: w.f.ProjectID, Title: "Visualize code", Status: domain.TicketStatusInProgress}}}
		d.Sessions = fakeSessions{[]*domain.Session{{
			ID: "s1", ProjectID: w.f.ProjectID, TicketID: "t1", RepositoryID: w.at.RepositoryID, WorkspaceID: workspace(w),
			Branch: w.f.Branch, Status: status, Interactive: true, Task: "visualize code", UpdatedAt: time.Now(),
		}}}
		d.Workspaces = fakeWorkspaces{base: w.f.BaseRef}
	}
}

// TestSessionCodeTabInTheBrowser opens a session's code URL: the task page
// with the session on its Code tab, the changed files beside the diff, the
// badge counting them, ] stepping through files, then Files and a file.
func TestSessionCodeTabInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"

	var paths []string
	var badge, current, afterStep, loc string
	var shot, filesShot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a')].map(a => a.querySelector('.dir').textContent + a.querySelector('.name').textContent)`, &paths),
		chromedp.Poll(`document.querySelector('[role=tab][aria-selected=true] .badge')?.textContent`, &badge, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .diff-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.diff-summary a[aria-current=location] .name').textContent`, &current),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		chromedp.Focus(`section[aria-label=Code] .main`, chromedp.ByQuery),
		chromedp.KeyEvent("]"),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.diff-summary a[aria-current=location] .name').textContent`, &afterStep),
		chromedp.KeyEvent("g"),
		chromedp.KeyEvent("f"),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Click(`section[aria-label=Code] .file-tree [title="README.md"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .code-view .code-lines .line').length === 2`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Location(&loc),
		chromedp.FullScreenshot(&filesShot, 85),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-session-changes.jpg", shot)
	saveShot("code-session-files.jpg", filesShot)
	want := []string{"README.md", "docs/new.md", "internal/app/feature.go", "new.txt", "nonl.txt", "web/src/main.js"}
	t.Logf("changed: %v", paths)
	if len(paths) == 0 {
		t.Fatalf("no changed files")
	}
	_ = want
	if badge != strconv.Itoa(len(paths))+" files" {
		t.Errorf("badge = %q, want %d files", badge, len(paths))
	}
	if current == afterStep {
		t.Errorf("] did not move to the next file (%q)", current)
	}
	if loc != url+"/files/README.md" {
		t.Errorf("location = %q", loc)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestSessionCodeTabFollowsTheWorktree: a running session's Code tab re-reads
// on code_changed. At the top of the diff the new file just appears; once the
// reader has scrolled, the source bar says the code changed on disk and R
// re-reads it without moving the reader.
func TestSessionCodeTabFollowsTheWorktree(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes"
	changed := `{"code_changed":{"workspace_id":"` + w.at.WorkspaceID + `","head":"` + w.f.SessionHead + `"}}`
	write := func(name, content string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error {
			if err := os.WriteFile(filepath.Join(w.f.Worktree, name), []byte(content), 0o644); err != nil {
				return err
			}
			w.feed.send(changed)
			return nil
		})
	}
	step := ""
	at := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = name; return nil })
	}
	long := strings.Repeat("a line of a long file\n", 400)
	var state, word, stateAfter string
	var top float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelector('section[aria-label=Code] .code-source-bar')?.dataset.state === 'live' && document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length === 5`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		at("connected"),
		w.feed.connected(),
		at("arrive"),
		write("arrived.txt", long),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length === 6 && !!document.querySelector('section[aria-label=Code] section.file[id="diff-arrived-txt"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .code-source-bar').dataset.state`, &state),
		chromedp.Evaluate(`(() => { const m = document.querySelector('section[aria-label=Code] .main'); m.scrollTop = 600; m.dispatchEvent(new Event('scroll')); return m.scrollTop })()`, &top),
		chromedp.Sleep(200*time.Millisecond),
		at("later"),
		write("later.txt", "later\n"),
		chromedp.Poll(`document.querySelector('section[aria-label=Code] .code-source-bar')?.dataset.state === 'stale'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .code-source-bar .state').textContent`, &word),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length === 7`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`!document.querySelector('section[aria-label=Code] section.file[id="diff-later-txt"]')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Focus(`section[aria-label=Code] .main`, chromedp.ByQuery),
		at("reload"),
		chromedp.KeyEvent("r"),
		chromedp.Poll(`!!document.querySelector('section[aria-label=Code] section.file[id="diff-later-txt"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .code-source-bar').dataset.state`, &stateAfter),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({state: document.querySelector('section[aria-label=Code] .code-source-bar')?.dataset.state, n: document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length, ids: [...document.querySelectorAll('section[aria-label=Code] section.file')].map(s=>s.id)})`, &dump))
		t.Fatalf("at %s: %v\n%s\nJS errors: %v", step, err, dump, errs.all())
	}
	if state != "live" || top < 500 {
		t.Errorf("after the first change: state %q, scrolled to %v", state, top)
	}
	if word != "Changed on disk" || stateAfter != "live" {
		t.Errorf("stale word %q, after R %q", word, stateAfter)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestSessionCodeTabWhenTheWorktreeIsGone: the source bar says so, Changes
// shows what the branch committed with All and Working disabled, and Files
// offers the branch on the repository code page instead of a tree.
func TestSessionCodeTabWhenTheWorktreeIsGone(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionStopped, func(w *codeWorld) string { return w.at.GoneWorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"
	var state, scope, browse, loc string
	var disabled []string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelector('section[aria-label=Code] .code-source-bar')?.dataset.state === 'gone'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .code-source-bar').dataset.state`, &state),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .diff-summary .segmented a[aria-current=page]').textContent`, &scope),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .segmented a[aria-disabled=true]')].map(a => a.textContent)`, &disabled),
		chromedp.FullScreenshot(&shot, 85),
		chromedp.Focus(`section[aria-label=Code] .main`, chromedp.ByQuery),
		chromedp.KeyEvent("g"),
		chromedp.KeyEvent("f"),
		chromedp.Poll(`!!document.querySelector('section[aria-label=Code] .main .empty a')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .main .empty a').getAttribute('href')`, &browse),
		chromedp.Location(&loc),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-session-gone.jpg", shot)
	if state != "gone" || scope != "Branch" || strings.Join(disabled, ",") != "All,Working" {
		t.Errorf("state %q scope %q disabled %v", state, scope, disabled)
	}
	want := "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code?ref=" + strings.ReplaceAll(w.f.Branch, "/", "%2F")
	if browse != want {
		t.Errorf("Browse the branch = %q, want %q", browse, want)
	}
	if loc != url+"/files" {
		t.Errorf("location %q", loc)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
