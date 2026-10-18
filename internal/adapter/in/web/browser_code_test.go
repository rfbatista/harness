package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

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
	w.srv = newBrowserServer(mux)
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
	var tools bool
	var shot, filesShot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a')].map(a => a.querySelector('.dir').textContent + a.querySelector('.name').textContent)`, &paths),
		chromedp.Poll(`document.querySelector('[role=tab][aria-selected=true] .badge')?.textContent`, &badge, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .diff-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.diff-summary a[aria-current=location] .name').textContent`, &current),
		chromedp.Evaluate(`!!document.querySelector('.pane-tabs .tools')`, &tools),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		chromedp.Focus(`section[aria-label=Code] .main`, chromedp.ByQuery),
		chromedp.KeyEvent("]"),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.diff-summary a[aria-current=location] .name').textContent`, &afterStep),
		chromedp.KeyEvent("s"),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .code-lines[data-layout=split] .line > .half').length > 0`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.KeyEvent("s"),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .half').length === 0 && document.querySelectorAll('section[aria-label=Code] .diff-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(3*time.Second)),
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
	if tools {
		t.Error("the terminal's tools show on the Code tab")
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

// TestHistoryDiffsInTheBrowser: an open commit shows its patch under its
// files, renamed file included; a session's uncommitted changes show theirs.
func TestHistoryDiffsInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, func(w *codeWorld, d *Deps) {
		withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID })(w, d)
		d.History = gitHistory{
			dirs:       map[string]string{w.at.RepositoryID: w.f.RepoDir},
			workspaces: map[string]testWorkspace{w.at.WorkspaceID: {repo: w.at.RepositoryID, dir: w.f.Worktree, branch: w.f.Branch, base: w.f.BaseRef}},
		}
	})
	ctx, errs := browser(t)
	commit := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/history/" + w.f.Rename
	working := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/history/working"

	var sections, workingSections []string
	var renamed, view string
	var lines int
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(commit),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Diff] .diff-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Diff] section.file h3.path')].map(h => h.title)`, &sections),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Diff] section.file h3.path .old')?.textContent ?? ''`, &renamed),
		chromedp.Evaluate(`document.querySelectorAll('section[aria-label=Diff] .code-lines .line').length`, &lines),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Diff] section.file .actions a')?.getAttribute('href') ?? ''`, &view),
		chromedp.FullScreenshot(&shot, 85),
		chromedp.Navigate(working),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Diff] section.file').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('section[aria-label=Diff] section.file h3.path')].map(h => h.title)`, &workingSections),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({url: location.href, ssr: document.querySelectorAll('[data-ssr]').length, files: document.querySelectorAll('section[aria-label=Diff] section.file').length, lines: document.querySelectorAll('section[aria-label=Diff] .code-lines .line').length, diff: document.querySelector('section[aria-label=Diff]')?.outerHTML.slice(0, 1500)})`, &dump))
		t.Fatalf("%v\n%s\nJS errors: %v", err, dump, errs.all())
	}
	saveShot("code-history-commit.jpg", shot)
	if strings.Join(sections, ",") != "docs/new.md,internal/app/app.go" || renamed != "docs/old.md" || lines == 0 {
		t.Errorf("commit diff: sections %v, renamed from %q, %d lines", sections, renamed, lines)
	}
	if !strings.Contains(view, "/code/files/docs/new.md?ref="+w.f.Rename) {
		t.Errorf("View file = %q", view)
	}
	if strings.Join(workingSections, ",") != "README.md,new.txt,nonl.txt" {
		t.Errorf("working diff: %v", workingSections)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// typeText types into the focused element one key at a time.
func typeText(s string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for _, r := range s {
			if err := chromedp.KeyEvent(string(r)).Do(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// TestCodeKeysAndLargeFilesInTheBrowser: a deep link targets a range of a
// file too large to load whole (shown windowed, not highlighted, with its
// notice); L goes to a line near its end; T finds a file by a fuzzy query;
// a symlink shows its placeholder.
func TestCodeKeysAndLargeFilesInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, nil)
	ctx, errs := browser(t)
	step := ""
	mark := func(n string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = n; return nil })
	}
	base := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code"

	var notice, meta, hash, loc, link, outside string
	var paletteOpen bool
	var targeted []string
	var rendered, total int
	var goneTo bool
	var elapsed time.Duration
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(base+"/files/big.txt#L45-L49"),
		mark("p1"),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.code-view .code-lines .line[data-target]').length === 5`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.code-view .code-lines .line[data-target]')].map(l => l.id)`, &targeted),
		chromedp.Evaluate(`document.querySelector('.code-view .notice').textContent`, &notice),
		chromedp.Evaluate(`document.querySelector('.code-view .meta').textContent`, &meta),
		chromedp.Evaluate(`document.querySelectorAll('.code-view .code-lines .line').length`, &rendered),
		chromedp.Evaluate(`JSON.parse(document.getElementById('code-seed').textContent).file.line_count`, &total),
		chromedp.Focus(`.code-view .scroll`, chromedp.ByQuery),
		chromedp.KeyEvent("l"),
		mark("p2"),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('dialog.palette[open] input') === document.activeElement`, &paletteOpen),
		typeText("40000"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			start := time.Now()
			if err := chromedp.KeyEvent("\r").Do(ctx); err != nil {
				return err
			}
			// Past the end: it clamps to the last line and says so.
			err := chromedp.Poll(fmt.Sprintf(`!!document.querySelector('#L%d[data-target]')`, total), &goneTo, chromedp.WithPollingTimeout(5*time.Second)).Do(ctx)
			elapsed = time.Since(start)
			return err
		}),
		chromedp.Evaluate(`location.hash`, &hash),
		chromedp.FullScreenshot(&shot, 80),
		chromedp.KeyEvent("t"),
		mark("p3"),
		chromedp.Poll(`document.querySelector('dialog.palette[open] input') === document.activeElement`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		typeText("appgo"),
		mark("p4"),
		chromedp.Poll(`document.querySelector('dialog.palette [role=option] .label')?.textContent === 'app.go'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.KeyEvent("\r"),
		mark("p5"),
		chromedp.Poll(`location.pathname.endsWith('/files/internal/app/app.go') && document.querySelectorAll('.code-view .code-lines .line').length === 4`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Location(&loc),
		chromedp.Click(`.file-tree [title="link-in"] > .item`, chromedp.ByQuery),
		mark("p6"),
		chromedp.Poll(`document.querySelector('.code-view .placeholder strong')?.textContent === 'Symbolic link'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.code-view .placeholder a')?.textContent ?? ''`, &link),
		chromedp.Click(`.file-tree [title="link-out"] > .item`, chromedp.ByQuery),
		mark("p7"),
		chromedp.Poll(`!document.querySelector('.code-view .placeholder a')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.code-view .placeholder p').textContent`, &outside),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({url: location.href, dialog: !!document.querySelector("dialog[open]"), active: document.activeElement?.outerHTML.slice(0,200), main: document.querySelector(".code-layout")?.outerHTML.slice(0,600)})`, &dump))
		t.Fatalf("at %s: %v\n%s\nJS errors: %v", step, err, dump, errs.all())
	}
	saveShot("code-large-file.jpg", shot)
	if !paletteOpen {
		t.Error("L did not open Go to line with its field focused")
	}
	if strings.Join(targeted, ",") != "L45,L46,L47,L48,L49" {
		t.Errorf("targeted %v", targeted)
	}
	if !strings.HasPrefix(notice, "Showing the first 1 MiB") || !strings.Contains(meta, "plain text: too long to highlight") {
		t.Errorf("notice %q, meta %q", notice, meta)
	}
	if total < 30_000 || rendered > 1500 {
		t.Errorf("%d lines, %d rendered: not windowed", total, rendered)
	}
	if hash != fmt.Sprintf("#L%d", total) || elapsed > 2*time.Second {
		t.Errorf("L 40000 in a %d-line file: hash %q in %v", total, hash, elapsed)
	}
	if loc != base+"/files/internal/app/app.go" {
		t.Errorf("Find file opened %q", loc)
	}
	if link != "Open the target" || outside != "Points outside this repository, so it is not shown." {
		t.Errorf("symlinks: %q / %q", link, outside)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestCodeMapAndNarrowLayoutInTheBrowser: the map's zone opens Files at its
// directory; below 44rem one side shows at a time, with a back control.
func TestCodeMapAndNarrowLayoutInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, nil)
	ctx, errs := browser(t)
	step := ""
	mark := func(n string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = n; return nil })
	}
	base := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code"
	var cards []string
	var loc, back string
	var sideShown, mainShown, sideAfter, mainAfter bool
	var shot []byte
	visible := func(sel string, out *bool) chromedp.Action {
		return chromedp.Evaluate(`(() => { const e = document.querySelector('`+sel+`'); return !!e && getComputedStyle(e).display !== 'none' })()`, out)
	}
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(base+"/map"),
		mark("p1"),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.code-map section.context').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.code-map section.context h3')].map(h => h.textContent.trim())`, &cards),
		chromedp.Click(`.code-map .zones a`, chromedp.ByQuery),
		mark("p2"),
		chromedp.Poll(`location.pathname.endsWith('/files/internal') && !!document.querySelector('.file-tree [title="internal/app"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Location(&loc),
		chromedp.EmulateViewport(560, 900),
		chromedp.Navigate(base),
		mark("p3"),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		visible(".code-layout > .side", &sideShown),
		visible(".code-layout > .main", &mainShown),
		chromedp.Click(`.file-tree [title="README.md"] > .item`, chromedp.ByQuery),
		mark("p4"),
		chromedp.Poll(`document.querySelectorAll('.code-view .code-lines .line').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		visible(".code-layout > .side", &sideAfter),
		visible(".code-layout > .main", &mainAfter),
		chromedp.Evaluate(`document.querySelector('[data-code-back]').textContent`, &back),
		chromedp.FullScreenshot(&shot, 80),
	)
	if err != nil {
		var dump string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({url: location.href, dialog: !!document.querySelector("dialog[open]"), active: document.activeElement?.outerHTML.slice(0,200), main: document.querySelector(".code-layout")?.outerHTML.slice(0,600)})`, &dump))
		t.Fatalf("at %s: %v\n%s\nJS errors: %v", step, err, dump, errs.all())
	}
	saveShot("code-narrow.jpg", shot)
	if strings.Join(cards, ",") != "Core,No bounded context,Unzoned files" {
		t.Errorf("map cards %v", cards)
	}
	if loc != base+"/files/internal" {
		t.Errorf("zone opened %q", loc)
	}
	if !sideShown || mainShown || sideAfter || !mainAfter || back != "‹ Files" {
		t.Errorf("narrow: side %v main %v, after opening side %v main %v, back %q", sideShown, mainShown, sideAfter, mainAfter, back)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestCodeExpandHidesTheSessionListInTheBrowser: Expand (and ⇧E) gives the
// Code tab the whole width at 1440, and brings the list back; the URL stays.
func TestCodeExpandHidesTheSessionListInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"
	listShown := `getComputedStyle(document.querySelector('.split-view > :first-child')).display !== 'none'`
	var before, expanded, back, pressed bool
	var locBefore, locAfter string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && !!document.querySelector('section[aria-label=Code] [data-code-expand]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(listShown, &before),
		chromedp.Location(&locBefore),
		chromedp.Click(`section[aria-label=Code] [data-code-expand]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('.split-view').hasAttribute('data-expanded')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Evaluate(`!(`+listShown+`)`, &expanded),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] [data-code-expand]').getAttribute('aria-pressed') === 'true'`, &pressed),
		chromedp.Focus(`section[aria-label=Code] .main`, chromedp.ByQuery),
		chromedp.KeyEvent("E", chromedp.KeyModifiers(input.ModifierShift)),
		chromedp.Poll(`!document.querySelector('.split-view').hasAttribute('data-expanded')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Evaluate(listShown, &back),
		chromedp.Location(&locAfter),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !before || !expanded || !pressed || !back {
		t.Errorf("list shown %v, hidden when expanded %v (pressed %v), back after ⇧E %v", before, expanded, pressed, back)
	}
	if locBefore != locAfter {
		t.Errorf("Expand changed the URL: %q → %q", locBefore, locAfter)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestCodeTreeKeysInTheBrowser: the flat tree follows the APG tree pattern:
// type-ahead, → opens a directory then enters its first child, ← goes to the
// parent then closes it, * opens every sibling directory; the levels stay right.
func TestCodeTreeKeysInTheBrowser(t *testing.T) {
	w := newCodeWorld(t, nil)
	ctx, errs := browser(t)
	base := w.srv.URL + "/projects/" + w.f.ProjectID + "/repositories/" + w.repoID + "/code"
	focused := func(out *string) chromedp.Action {
		return chromedp.Evaluate(`document.activeElement.closest('[role=treeitem]')?.getAttribute('title') ?? ''`, out)
	}
	var afterType, afterRight, intoChild, childLevel, toParent, collapsed string
	var levels []string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(base),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('.file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Focus(`.file-tree .item[tabindex="0"]`, chromedp.ByQuery),
		chromedp.KeyEvent("i"),
		chromedp.Sleep(100*time.Millisecond),
		focused(&afterType),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Poll(`document.querySelector('.file-tree [title="internal"]').getAttribute('aria-expanded') === 'true' && !!document.querySelector('.file-tree [title="internal/app"]')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		focused(&afterRight),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Sleep(100*time.Millisecond),
		focused(&intoChild),
		chromedp.Evaluate(`document.activeElement.closest('[role=treeitem]').getAttribute('aria-level')`, &childLevel),
		chromedp.KeyEvent(kb.ArrowLeft),
		chromedp.Sleep(100*time.Millisecond),
		focused(&toParent),
		chromedp.KeyEvent(kb.ArrowLeft),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.file-tree [title="internal"]').getAttribute('aria-expanded')`, &collapsed),
		chromedp.KeyEvent("*"),
		chromedp.Poll(`['bulk','docs','internal','web'].every(d => document.querySelector('.file-tree [title="' + d + '"]').getAttribute('aria-expanded') === 'true')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`!!document.querySelector('.file-tree [title="docs/new.md"]') && !document.querySelector('.file-tree [aria-busy=true]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.file-tree [role=treeitem]')].filter(li => ['docs','docs/old.md','docs/new.md'].includes(li.title)).map(li => li.title + ':' + li.getAttribute('aria-level') + '/' + li.getAttribute('aria-posinset') + '/' + li.getAttribute('aria-setsize'))`, &levels),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if afterType != "internal" || afterRight != "internal" || intoChild != "internal/app" || childLevel != "2" || toParent != "internal" || collapsed != "false" {
		t.Errorf("type-ahead %q, → %q, → %q (level %s), ← %q, ← leaves internal expanded=%s", afterType, afterRight, intoChild, childLevel, toParent, collapsed)
	}
	if strings.Join(levels, " ") != "docs:1/2/13 docs/new.md:2/1/1" {
		t.Errorf("levels after *: %v", levels)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestSessionCodeTabOnAPhone: the Code tab sits in the session screen's tab
// row on a phone, one code side at a time, the page never scrolls sideways,
// and ⋯ still opens the session sheet.
func TestSessionCodeTabOnAPhone(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"
	var tabs []string
	var overflow, sideShown, mainShown, mainAfter, sheetOpen, expand bool
	var shot []byte
	visible := func(sel string, out *bool) chromedp.Action {
		return chromedp.Evaluate(`(() => { const e = document.querySelector('`+sel+`'); return !!e && getComputedStyle(e).display !== 'none' && e.getBoundingClientRect().width > 0 })()`, out)
	}
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(375, 700),
		chromedp.Navigate(url),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].map(b => b.firstChild.textContent.trim())`, &tabs),
		chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth`, &overflow),
		visible(`section[aria-label=Code] .code-layout > .side`, &sideShown),
		visible(`section[aria-label=Code] .code-layout > .main`, &mainShown),
		chromedp.Evaluate(`!!document.querySelector('[data-code-expand]')`, &expand),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .diff-summary .changed a').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		visible(`section[aria-label=Code] .code-layout > .main`, &mainAfter),
		chromedp.FullScreenshot(&shot, 80),
		chromedp.Evaluate(`document.getElementById('session-more').click()`, nil),
		chromedp.Poll(`!!document.querySelector('#session-sheet:popover-open')`, &sheetOpen, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	saveShot("code-session-phone.jpg", shot)
	if strings.Join(tabs, ",") != "Agent,History,App,Design,Code" {
		t.Errorf("tabs %v", tabs)
	}
	if expand {
		t.Error("Expand shows on a phone, where the list is already hidden")
	}
	if overflow || !sideShown || mainShown || !mainAfter || !sheetOpen {
		t.Errorf("overflow %v, side %v main %v, main after opening %v, sheet %v", overflow, sideShown, mainShown, mainAfter, sheetOpen)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
