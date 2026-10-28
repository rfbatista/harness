package web

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// The person's numbers for the Code view (ADR 0044): a session that changed 500
// files shows its first page within a second, and no main-thread task runs over
// the long-task cap while the view loads, grows, scrolls, refreshes and selects.
const (
	changedFiles    = 500
	linesPerFile    = 60
	firstPageWithin = 1000 * time.Millisecond
	longTaskCapMs   = 100.0
	wideEntries     = 1000
)

// longTaskObserver is installed before the page's own scripts run, so a task
// during load is seen. longtask entries start at 50 ms; the cap is judged on them.
const longTaskObserver = `
window.__longTasks = [];
new PerformanceObserver((list) => {
  for (const e of list.getEntries()) window.__longTasks.push({ ms: Math.round(e.duration), at: Math.round(e.startTime) });
}).observe({ type: 'longtask', buffered: true });
window.__firstPageAt = 0;
(function watch() {
  if (!window.__firstPageAt) {
    const rows = document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length;
    const lines = document.querySelectorAll('section[aria-label=Code] .diff-view .code-lines .line').length;
    if (rows >= 50 && lines > 0 && document.querySelectorAll('[data-ssr]').length === 0) window.__firstPageAt = Math.round(performance.now());
  }
  requestAnimationFrame(watch);
})();
`

// writeChangedFiles gives the session's worktree 500 new files of 60 lines (about
// 30 000 changed lines), five binary ones among them (the diff withholds those).
func writeChangedFiles(t *testing.T, worktree string) {
	t.Helper()
	for i := 0; i < changedFiles; i++ {
		dir := filepath.Join(worktree, "bulk", fmt.Sprintf("d%02d", i/25))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for l := 0; l < linesPerFile; l++ {
			fmt.Fprintf(&b, "export const value%d_%d = %q; // line %d of file %d\n", i, l, "text", l, i)
		}
		name := fmt.Sprintf("f%04d.js", i)
		content := []byte(b.String())
		if i%100 == 7 {
			name = fmt.Sprintf("f%04d.bin", i)
			content = append([]byte{0, 1, 2, 3, 0, 0xff}, content...)
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// commitWideDirectory puts a directory of 1 000 entries in the repository's main
// branch and brings it into the session's worktree, so the tree has the
// entries and the diff does not count them.
func commitWideDirectory(t *testing.T, repo, worktree string) {
	t.Helper()
	wide := filepath.Join(repo, "wide")
	if err := os.MkdirAll(wide, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < wideEntries; i++ {
		if err := os.WriteFile(filepath.Join(wide, fmt.Sprintf("entry%04d.txt", i)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(repo, "add", "wide")
	git(repo, "commit", "-q", "-m", "a wide directory")
	git(worktree, "merge", "-q", "--no-edit", "main")
}

type longTask struct {
	Ms int `json:"ms"`
	At int `json:"at"`
}

// TestCodeViewStaysResponsiveWith500ChangedFiles is the acceptance test of
// ADR 0044: the first page within a second, whole-diff totals in the heading,
// Show more and the scroll reaching the last page, a selection, a refresh and a
// directory of 1 000 entries, with no main-thread task over the 100 ms cap.
func TestCodeViewStaysResponsiveWith500ChangedFiles(t *testing.T) {
	w := newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
	commitWideDirectory(t, w.f.RepoDir, w.f.Worktree)
	writeChangedFiles(t, w.f.Worktree)
	ctx, errs := browser(t)
	url := w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code/changes"
	changed := `{"code_changed":{"workspace_id":"` + w.at.WorkspaceID + `","head":"` + w.f.SessionHead + `"}}`

	var tasks []longTask
	checkpoint := func(label string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var got []longTask
			if err := chromedp.Evaluate(`window.__longTasks`, &got).Do(ctx); err != nil {
				return err
			}
			for _, lt := range got[len(tasks):] {
				t.Logf("long task after %s: %d ms at %d ms", label, lt.Ms, lt.At)
			}
			tasks = got
			return nil
		})
	}
	var heading string
	var firstPageAt float64
	var rowsAfterScroll, rowsAfterMore int
	var topBefore, topAfter float64
	var wideRows int
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(longTaskObserver).Do(ctx)
			return err
		}),
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(url),
		chromedp.Poll(`window.__firstPageAt > 0`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`window.__firstPageAt`, &firstPageAt),
		checkpoint("first page"),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .diff-summary .totals').textContent.trim()`, &heading),

		// The scroll reaches the last page: each page loads as the end of the loaded files nears.
		chromedp.Poll(`(() => {
			const m = document.querySelector('section[aria-label=Code] .main');
			m.scrollTop = m.scrollHeight;
			m.dispatchEvent(new Event('scroll'));
			return document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 50;
		})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length`, &rowsAfterScroll),
		checkpoint("scroll loading"),

		// Show more reaches the last page too.
		chromedp.Poll(`(() => {
			const b = document.querySelector('section[aria-label=Code] .diff-summary footer.more button');
			if (b) b.click();
			return !document.querySelector('section[aria-label=Code] .diff-summary footer.more');
		})()`, nil, chromedp.WithPollingTimeout(25*time.Second), chromedp.WithPollingInterval(300*time.Millisecond)),
		chromedp.Evaluate(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length`, &rowsAfterMore),
		checkpoint("show more"),

		// Scroll through the diff from the top, a screen at a time.
		chromedp.Evaluate(`(() => { const m = document.querySelector('section[aria-label=Code] .main'); m.scrollTop = 0; m.dispatchEvent(new Event('scroll')); return 0 })()`, nil),
		chromedp.Poll(`(() => {
			const m = document.querySelector('section[aria-label=Code] .main');
			m.scrollTop += 700;
			m.dispatchEvent(new Event('scroll'));
			return m.scrollTop + m.clientHeight >= m.scrollHeight - 2 || m.scrollTop > 60000;
		})()`, nil, chromedp.WithPollingTimeout(30*time.Second), chromedp.WithPollingInterval(40*time.Millisecond)),
		checkpoint("scroll"),

		// Select a file far down the list.
		chromedp.Evaluate(`document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a')[300].click()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		checkpoint("selection"),

		// A refresh while the reader is partway down keeps the place.
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .main').scrollTop`, &topBefore),
		w.feed.connected(),
		chromedp.ActionFunc(func(context.Context) error {
			if err := os.WriteFile(filepath.Join(w.f.Worktree, "bulk", "d05", "f0150.js"), []byte("export const refreshed = true;\n"), 0o644); err != nil {
				return err
			}
			w.feed.send(changed)
			return nil
		}),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('section[aria-label=Code] .main').scrollTop`, &topAfter),
		checkpoint("refresh"),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}

	t.Logf("first page visible after %.0f ms; heading %q; rows after scroll %d, after Show more %d", firstPageAt, heading, rowsAfterScroll, rowsAfterMore)
	if d := time.Duration(firstPageAt) * time.Millisecond; d > firstPageWithin {
		t.Errorf("the first page (50 files with lines) was visible after %v; want within %v", d, firstPageWithin)
	}
	if !strings.HasPrefix(heading, "5") || !strings.Contains(heading, " files") {
		t.Errorf("the heading says %q; want the whole diff's file count (505 files)", heading)
	}
	if rowsAfterScroll <= 50 || rowsAfterMore < changedFiles {
		t.Errorf("rows after scrolling %d, after Show more %d; want more than one page, then every file", rowsAfterScroll, rowsAfterMore)
	}
	if diff := topAfter - topBefore; diff > 200 || diff < -200 {
		t.Errorf("the scroll position moved from %v to %v on a refresh", topBefore, topAfter)
	}

	err = chromedp.Run(ctx,
		chromedp.Click(`section[aria-label=Code] nav.segmented a[href$="/code/files"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .file-tree [role=treeitem]').length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Click(`section[aria-label=Code] .file-tree [title="wide"] > .item`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelectorAll('section[aria-label=Code] .file-tree [role=treeitem][title^="wide/"]').length >= `+fmt.Sprint(wideEntries), nil, chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('section[aria-label=Code] .file-tree [role=treeitem][title^="wide/"]').length`, &wideRows),
		checkpoint("wide directory"),
	)
	if err != nil {
		t.Fatalf("opening the directory of %d entries: %v\nJS errors: %v", wideEntries, err, errs.all())
	}
	if wideRows < wideEntries {
		t.Errorf("the directory shows %d entries; want %d", wideRows, wideEntries)
	}

	for _, lt := range tasks {
		if float64(lt.Ms) > longTaskCapMs {
			t.Errorf("a main-thread task ran %d ms at %d ms (cap %.0f ms)", lt.Ms, lt.At, longTaskCapMs)
		}
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
