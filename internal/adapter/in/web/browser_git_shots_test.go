package web

import (
	"os"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// TestGitScreenshots saves the feature's pages, light and dark, to
// WEB_SCREENSHOT_DIR over the real worktree git fixture. It runs only then.
func TestGitScreenshots(t *testing.T) {
	if os.Getenv("WEB_SCREENSHOT_DIR") == "" {
		t.Skip("set WEB_SCREENSHOT_DIR to take the screenshots")
	}
	r := newRealGit(t)
	r.f.Git(t, r.f.Worktrees["ahead"], "mv", "README.md", "GUIDE.md")
	b := newGitBrowser(t)
	code := r.url + "/projects/p1/tasks/" + r.task + "/sessions/" + r.at.Sessions["ahead"] + "/code/changes"
	history := r.url + "/projects/p1/tasks/" + r.task + "/sessions/" + r.at.Sessions["ahead"] + "/history"
	for _, scheme := range []string{"light", "dark"} {
		scheme := scheme
		var task, taskPop, codeShot, panel, hist []byte
		b.run("scheme "+scheme, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}, {Name: "prefers-reduced-motion", Value: "reduce"}}),
			chromedp.EmulateViewport(1440, 900))
		b.run("task page", chromedp.Navigate(r.taskPage()), waitPoll(`document.querySelector('[data-ssr]') === null && `+shownJS(r.rowChip("ahead"))),
			chromedp.Sleep(10*time.Millisecond))
		_ = taskPop
		b.run("task shot", chromedp.Sleep(300*time.Millisecond), chromedp.FullScreenshot(&task, 90))
		b.run("popover", chromedp.Evaluate(clickJS(`#task-git-button`), nil), waitPoll(`document.getElementById('task-git').matches(':popover-open')`),
			chromedp.Evaluate(clickJS(`#task-git [data-fetch]`), nil), waitPoll(`document.querySelector('`+r.rowChip("behind")+`').dataset.state === 'behind'`),
			chromedp.Sleep(300*time.Millisecond), chromedp.FullScreenshot(&taskPop, 90))
		b.run("code", chromedp.Navigate(code), waitPoll(`document.querySelectorAll('[data-ssr]').length === 0 && document.querySelectorAll('section[aria-label=Code] .diff-summary .changed a').length > 0`),
			chromedp.Sleep(400*time.Millisecond), chromedp.FullScreenshot(&codeShot, 90))
		b.run("panel", chromedp.Evaluate(clickJS(`section[aria-label=Code] .code-source-bar .sync-slot > button.sync-state`), nil),
			waitPoll(`!!document.querySelector('section[aria-label=Code] .sync-panel:popover-open')`), chromedp.Sleep(300*time.Millisecond), chromedp.FullScreenshot(&panel, 90))
		b.run("history", chromedp.Navigate(history), waitPoll(shownJS(`.git-head .sync-slot > button.sync-state`)), chromedp.Sleep(400*time.Millisecond), chromedp.FullScreenshot(&hist, 90))
		saveShot("git-task-"+scheme+".jpg", task)
		saveShot("git-task-popover-"+scheme+".jpg", taskPop)
		saveShot("git-code-changes-"+scheme+".jpg", codeShot)
		saveShot("git-code-panel-"+scheme+".jpg", panel)
		saveShot("git-history-"+scheme+".jpg", hist)
	}
}
