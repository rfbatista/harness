package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// The session Code tab's review layout: the diff's layout switch, its split
// and its full screen. Each test opens the running session's Code tab.

const (
	codeSection  = `section[aria-label=Code]`
	layoutSwitch = codeSection + ` [data-layout-switch]`
	sideBySide   = `[...document.querySelectorAll('` + layoutSwitch + ` button')].find(b => b.textContent.trim() === 'Side by side')`
)

func codeTabURL(w *codeWorld) string {
	return w.srv.URL + "/projects/" + w.f.ProjectID + "/tasks/t1/sessions/s1/code"
}

func runningCodeWorld(t *testing.T) *codeWorld {
	t.Helper()
	return newCodeWorld(t, withSession(domain.SessionRunning, func(w *codeWorld) string { return w.at.WorkspaceID }))
}

// Unified | Side by side is a segmented switch; Side by side is remembered
// across a reload.
func TestTheDiffLayoutSwitchIsRememberedInTheBrowser(t *testing.T) {
	w := runningCodeWorld(t)
	ctx, errs := browser(t)
	ready := `document.querySelectorAll('[data-ssr]').length === 0 && !!document.querySelector('` + layoutSwitch + `')`

	var labels []string
	var pressedBefore, pressedAfter string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(ready, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('`+layoutSwitch+` button')].map(b => b.textContent.trim())`, &labels),
		chromedp.Evaluate(sideBySide+`.getAttribute('aria-pressed')`, &pressedBefore),
		chromedp.Evaluate(sideBySide+`.click()`, nil),
		chromedp.Poll(`document.querySelectorAll('`+codeSection+` .code-lines[data-layout=split] .line > .half').length > 0`, nil, chromedp.WithPollingTimeout(3*time.Second)),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(ready, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(sideBySide+`.getAttribute('aria-pressed')`, &pressedAfter),
		chromedp.Poll(`document.querySelectorAll('`+codeSection+` .code-lines[data-layout=split] .line > .half').length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if len(labels) != 2 || labels[0] != "Unified" || labels[1] != "Side by side" {
		t.Errorf("the switch reads %v, want [Unified Side by side]", labels)
	}
	if pressedBefore != "false" || pressedAfter != "true" {
		t.Errorf("Side by side pressed %q before choosing it, %q after a reload", pressedBefore, pressedAfter)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// Below 44rem the diff shows one side, and Side by side is disabled with its
// reason in the title.
func TestSideBySideIsDisabledInANarrowWindowInTheBrowser(t *testing.T) {
	w := runningCodeWorld(t)
	ctx, errs := browser(t)

	var disabled, title string
	var halves int
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(600, 900),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && !!document.querySelector('`+layoutSwitch+`')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(`!!`+sideBySide+` && `+sideBySide+`.getAttribute('aria-disabled') === 'true'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(sideBySide+`.getAttribute('aria-disabled')`, &disabled),
		chromedp.Evaluate(sideBySide+`.title`, &title),
		chromedp.Evaluate(sideBySide+`.click()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelectorAll('`+codeSection+` .half').length`, &halves),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if disabled != "true" || title != "Side by side needs a wider window" {
		t.Errorf("aria-disabled %q, title %q", disabled, title)
	}
	if halves != 0 {
		t.Errorf("a narrow window drew %d side-by-side halves", halves)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}
