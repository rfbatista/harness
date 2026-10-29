package web

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/adapter/out/filesystem"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The Files view's browser tests, over a real virtual file system: a git
// worktree on disk and a resources root opened for the session.

type filesTickets struct{ ports.TicketReader }

func (filesTickets) ListTickets(context.Context, string, ports.TicketFilter) ([]*domain.Ticket, error) {
	return nil, nil
}

type filesDocuments struct{ ports.DocumentReader }

func (filesDocuments) ListDocuments(string, domain.DocumentScope) []*domain.Document { return nil }

type filesSessions struct {
	ports.SessionRepository
	s *domain.Session
}

func (f filesSessions) Get(id string) *domain.Session {
	if id == f.s.ID {
		return f.s
	}
	return nil
}

type filesWorkspaces struct {
	ports.WorkspaceRepository
	path string
}

func (f filesWorkspaces) Get(_ context.Context, id string) *domain.Workspace {
	return &domain.Workspace{ID: id, Path: f.path}
}

// filesWorld is session s1 of the code world with its Files routes served by
// a real vfs.FileSystem over a worktree this test fills.
type filesWorld struct {
	*codeWorld
	worktree  string
	resources *vfs.Service
	sess      *domain.Session
	tab       string
}

func newFilesWorld(t *testing.T, status domain.SessionStatus, bigFolder int) *filesWorld {
	t.Helper()
	fw := &filesWorld{worktree: t.TempDir()}
	if out, err := exec.Command("git", "-C", fw.worktree, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(fw.worktree, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fw.worktree, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "node_modules/\n")
	write("node_modules/dep/index.js", "x\n")
	write("web/src/main.js", "export function main() {\n  return 1;\n}\n")
	write("web/blob.bin", "a\x00b")
	write("README.md", "# hello\n")
	for i := range bigFolder {
		write(fmt.Sprintf("big/f%05d.txt", i), "x\n")
	}
	fw.sess = &domain.Session{ID: "s1", WorkspaceID: "wf", Status: status}
	resources := vfs.New(vfs.Sources{Tickets: filesTickets{}, Documents: filesDocuments{}})
	fw.resources = resources
	events := vfs.NewRootEvents()
	resources.UseRootEvents(events)
	fsys := vfs.NewFileSystem(resources, filesSessions{s: fw.sess}, filesWorkspaces{path: fw.worktree}, filesystem.NewWorktreeFiles(), events)
	fsys.UseGitIgnore(gitcli.NewCode())
	if status != domain.SessionDone {
		dir, err := resources.Open(context.Background(), "s1", ports.ShellScope{ProjectID: "p1"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resources.Close("s1", dir) })
	}
	fw.codeWorld = newCodeWorld(t, func(w *codeWorld, d *Deps) {
		w.files = fsys
		withSession(status, func(w *codeWorld) string { return w.at.WorkspaceID })(w, d)
	})
	fw.tab = fw.srv.URL + "/projects/" + fw.f.ProjectID + "/tasks/t1?session=s1"
	return fw
}

func (fw *filesWorld) openFilesTab() chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(fw.tab),
		chromedp.Poll(`!![...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim() === 'Files')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim() === 'Files').click()`, nil),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Files] .file-tree [data-kind=root]').length === 2 && document.querySelector('section[aria-label=Files] .code-layout').offsetHeight > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
	}
}

// failFiles fails the test with what the Files tab shows.
func failFiles(t *testing.T, ctx context.Context, err error, errs *errorLog) {
	t.Helper()
	var dump string
	_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({text: document.querySelector('section[aria-label=Files]')?.innerText.slice(0, 600), tabs: [...document.querySelectorAll('.pane-tabs [role=tab]')].map(b => b.textContent.trim() + ':' + b.getAttribute('aria-selected')), page: (() => { const d = document.querySelector('section.session')._x_dataStack; return JSON.stringify(d.map(x => [Object.keys(x).length, x.detailTab, x.showingFiles, x.hasFiles])) })(), chain: (() => { const out = []; let e = document.querySelector('section[aria-label=Files] .files-pane'); while (e && out.length < 8) { const r = e.getBoundingClientRect(); out.push(e.tagName + '.' + e.className + ' ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' ' + getComputedStyle(e).display); e = e.parentElement } return out })(), boxes: ['.files-pane', '.code-layout', '.side', '.separator', '.main'].map(q => { const e = document.querySelector('section[aria-label=Files] ' + q); if (!e) return q + ' none'; const r = e.getBoundingClientRect(); return q + ' ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' ' + getComputedStyle(e).display }), rows: [...document.querySelectorAll('section[aria-label=Files] .file-tree li')].map(l => l.getAttribute('title') || l.className).slice(0, 40)})`, &dump))
	t.Fatalf("%v\n%s\nJS errors: %v", err, dump, errs.all())
}

const filesTree = `section[aria-label=Files] .file-tree`

func rowSel(title string) string { return filesTree + ` [title="` + title + `"] > .item` }

// TestFilesTabBrowsesTheTreeAndOpensAFile: both roots, the worktree open by
// itself, a folder loaded when expanded, an ignored folder tagged and
// collapsed, a file read-only in the Code tab's viewer, a binary file as a notice.
func TestFilesTabBrowsesTheTreeAndOpensAFile(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	step := ""
	at := func(n string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = n; return nil })
	}
	var roots, strip, tag string
	var gitListed bool
	var addons int
	var shot []byte
	err := chromedp.Run(ctx, append(fw.openFilesTab(),
		chromedp.Evaluate(`[...document.querySelectorAll('`+filesTree+` [data-kind=root] > .item .name > span:first-child')].map(n => n.textContent.trim()).join(',')`, &roots),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Files] .files-strip').dataset.state`, &strip),
		chromedp.Evaluate(`document.querySelector('`+filesTree+` [title="node_modules (ignored by git)"] .ignored')?.textContent`, &tag),
		chromedp.Evaluate(`!!document.querySelector('`+filesTree+` [title=".git"]')`, &gitListed),
		chromedp.Evaluate(`document.querySelector('`+filesTree+` [title="node_modules (ignored by git)"]').getAttribute('aria-expanded')`, new(string)),
		chromedp.Evaluate(`document.querySelectorAll('`+filesTree+` [data-kind=root] > .item > .actions, `+filesTree+` [data-kind=dir] > .item > .actions').length`, &addons),
		at("1"),
		chromedp.Click(rowSel("web"), chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web/src"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("2"),
		chromedp.Click(rowSel("web/src"), chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web/src/main.js"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("3"),
		chromedp.Click(rowSel("web/src/main.js"), chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Files] .code-view .code-lines .line').length === 3`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`document.querySelector('`+filesTree+` [title="web/src/main.js"]').getAttribute('aria-selected') === 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Poll(`!!document.querySelector('section[aria-label=Files] .code-view .head .ro')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.FullScreenshot(&shot, 85),
		at("4"),
		chromedp.Click(rowSel("web/blob.bin"), chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('section[aria-label=Files] .code-view .placeholder, section[aria-label=Files] .code-view .notice')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("5"),
		chromedp.Click(rowSel("node_modules (ignored by git)"), chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title^="node_modules/dep"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("6"),
		chromedp.Click(`section[aria-label=Files] .file-tree [data-root=resources] > .item`, chromedp.ByQuery),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title=".index.json"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)...)
	saveShot("files-tab.jpg", shot)
	if err != nil {
		t.Logf("at step %s", step)
		failFiles(t, ctx, err, errs)
	}
	if roots != "Worktree,Resources" || strip != "working" || tag != "ignored" || gitListed || addons < 3 {
		t.Errorf("roots %q, strip %q, tag %q, .git listed %v, action slots %d", roots, strip, tag, gitListed, addons)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestFilesTabPagesAFolderOfThousandsOfEntries: 3000 entries arrive page by
// page as the tree scrolls, with no long task over 200 ms.
func TestFilesTabPagesAFolderOfThousandsOfEntries(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionIdle, 3000)
	ctx, errs := browser(t)
	var loaded int
	var longest float64
	err := chromedp.Run(ctx, append(fw.openFilesTab(),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="big"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__long = []; new PerformanceObserver(l => l.getEntries().forEach(e => window.__long.push(e.duration))).observe({entryTypes: ['longtask']})`, nil),
		chromedp.Click(rowSel("big"), chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('`+filesTree+` [title^="big/"]').length >= 200`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`(() => { const s = document.querySelector('section[aria-label=Files] .side'); s.scrollTop = s.scrollHeight; return document.querySelectorAll('`+filesTree+` [title^="big/"]').length === 3000 })()`, nil, chromedp.WithPollingTimeout(40*time.Second), chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(`document.querySelectorAll('`+filesTree+` [title^="big/"]').length`, &loaded),
		chromedp.Evaluate(`Math.max(0, ...window.__long)`, &longest),
	)...)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if loaded != 3000 || longest > 200 {
		t.Errorf("loaded %d entries, longest task %.0f ms", loaded, longest)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestFilesTabShowsRootsAndAgentStateLive: an ended session's resources root
// is marked with its note, and with the tab open the stream moves the strip
// and the root without a reload.
func TestFilesTabShowsRootsAndAgentStateLive(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionRunning, 0)
	ctx, errs := browser(t)
	var note, strip string
	err := chromedp.Run(ctx, append(fw.openFilesTab(),
		fw.feed.connected(),
		chromedp.ActionFunc(func(context.Context) error {
			fw.feed.send(`{"session":{"id":"s1","project_id":"` + fw.f.ProjectID + `","ticket_id":"t1","status":"waiting_approval","workspace_id":"` + fw.at.WorkspaceID + `"}}`)
			return nil
		}),
		chromedp.Poll(`document.querySelector('section[aria-label=Files] .files-strip').dataset.state === 'waiting'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.ActionFunc(func(context.Context) error {
			fw.feed.send(`{"session_files_root":{"type":"session_files_root","session_id":"s1","root":"resources","available":false,"note":"The resources folder ended with the session."}}`)
			return nil
		}),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [data-root=resources][aria-disabled=true] ~ .root-note, `+filesTree+` .root-note')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+filesTree+` .root-note').textContent`, &note),
		chromedp.ActionFunc(func(context.Context) error {
			fw.feed.send(`{"session":{"id":"s1","project_id":"` + fw.f.ProjectID + `","ticket_id":"t1","status":"done","workspace_id":"` + fw.at.WorkspaceID + `"}}`)
			return nil
		}),
		chromedp.Poll(`document.querySelector('section[aria-label=Files] .files-strip').dataset.state === 'ended'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Files] .files-strip').dataset.state`, &strip),
	)...)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if note != "The resources folder ended with the session." || strip != "ended" {
		t.Errorf("note %q, strip %q", note, strip)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestFilesTabOfAnEndedSessionMarksTheResourcesRoot: no resources root is
// open for a finished session, and the worktree still browses.
func TestFilesTabOfAnEndedSessionMarksTheResourcesRoot(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionDone, 0)
	ctx, errs := browser(t)
	var note string
	err := chromedp.Run(ctx, append(fw.openFilesTab(),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+filesTree+` .root-note').textContent`, &note),
	)...)
	if err != nil {
		failFiles(t, ctx, err, errs)
	}
	if note != "The resources folder ended with the session." {
		t.Errorf("note %q", note)
	}
}

// TestFilesTabDividerResizesAndTheViewerExpands: the separator moves with the
// arrow keys, the width survives a reload, a double click restores the
// default, and the viewer takes the whole tab and gives it back.
func TestFilesTabDividerResizesAndTheViewerExpands(t *testing.T) {
	fw := newFilesWorld(t, domain.SessionIdle, 0)
	ctx, errs := browser(t)
	const sep = `section[aria-label=Files] .code-layout > .separator`
	step := ""
	at := func(n string) chromedp.Action {
		return chromedp.ActionFunc(func(context.Context) error { step = n; return nil })
	}
	var role, before, after, reloaded, reset string
	var treeHidden, treeBack bool
	var stored string
	err := chromedp.Run(ctx, append(fw.openFilesTab(),
		at("1"),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+sep+`').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('`+sep+`').getAttribute('aria-valuenow')`, &before),
		at("2"),
		chromedp.Focus(sep, chromedp.ByQuery),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.KeyEvent(kb.ArrowRight),
		at("3"),
		chromedp.Poll(`document.querySelector('`+sep+`').getAttribute('aria-valuenow') !== '`+before+`'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+sep+`').getAttribute('aria-valuenow')`, &after),
		chromedp.Evaluate(`localStorage.getItem('harness:files.split') ?? ''`, &stored),
		chromedp.Navigate(fw.tab),
		at("4"),
		chromedp.Poll(`!![...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim() === 'Files')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.pane-tabs [role=tab]')].find(b => b.textContent.trim() === 'Files').click()`, nil),
		at("5"),
		chromedp.Poll(`!!document.querySelector('`+sep+`')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+sep+`').getAttribute('aria-valuenow')`, &reloaded),
		chromedp.Sleep(400*time.Millisecond), // the layout is measured again after the reload
		chromedp.Evaluate(`document.querySelector('`+sep+`').dispatchEvent(new MouseEvent('dblclick', {bubbles: true}))`, nil),
		at("6"),
		chromedp.Poll(`localStorage.getItem('harness:files.split') === null`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`document.querySelector('`+sep+`').getAttribute('aria-valuenow')`, &reset),
		at("7"),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		at("8"),
		chromedp.Click(rowSel("web"), chromedp.ByQuery),
		at("9"),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web/src"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("10"),
		chromedp.Click(rowSel("web/src"), chromedp.ByQuery),
		at("11"),
		chromedp.Poll(`!!document.querySelector('`+filesTree+` [title="web/src/main.js"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("12"),
		chromedp.Click(rowSel("web/src/main.js"), chromedp.ByQuery),
		at("13"),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Files] .code-view .code-lines .line').length === 3`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		at("14"),
		chromedp.Click(`section[aria-label=Files] [data-files-expand]`, chromedp.ByQuery),
		at("15"),
		chromedp.Poll(`document.querySelector('section[aria-label=Files] .code-layout').hasAttribute('data-expanded')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('section[aria-label=Files] .code-layout > .side')).display === 'none'`, &treeHidden),
		at("16"),
		chromedp.Click(`section[aria-label=Files] [data-files-expand]`, chromedp.ByQuery),
		at("17"),
		chromedp.Poll(`!document.querySelector('section[aria-label=Files] .code-layout').hasAttribute('data-expanded')`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('section[aria-label=Files] .code-layout > .side')).display !== 'none'`, &treeBack),
	)...)
	if err != nil {
		t.Logf("at step %s: before %q, after %q, stored %q, reloaded %q", step, before, after, stored, reloaded)
		failFiles(t, ctx, err, errs)
	}
	if role != "separator" || before == after || stored == "" || reloaded != after || reset == after || !treeHidden || !treeBack {
		t.Errorf("role %q, value %q -> %q (stored %q), after a reload %q, after a double click %q (the default again), tree hidden %v, tree back %v", role, before, after, stored, reloaded, reset, treeHidden, treeBack)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
