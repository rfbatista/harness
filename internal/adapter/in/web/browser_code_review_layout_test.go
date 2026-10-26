package web

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

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

const (
	splitSeparator = codeSection + ` .code-layout > [role=separator]`
	splitSide      = codeSection + ` .code-layout > .side`
)

// The split between the file list and the diff: it drags inside its limits,
// double-click and Home reset it, the arrows move it, and a reload keeps it.
func TestTheCodeSplitResizesAndIsRememberedInTheBrowser(t *testing.T) {
	w := runningCodeWorld(t)
	ctx, errs := browser(t)
	ready := `document.querySelectorAll('[data-ssr]').length === 0 && !!document.querySelector('` + splitSeparator + `')`
	sideWidth := `document.querySelector('` + splitSide + `').getBoundingClientRect().width`
	layoutWidth := `document.querySelector('` + codeSection + ` .code-layout').getBoundingClientRect().width`
	const rem = 16.0

	var startW, draggedW, wideW, narrowW, afterKey, resetW, reloadedW, hit, layoutW float64
	var role, orientation, nowStart, nowDragged, nowAfterReload string
	var focusable bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(ready, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(sideWidth, &startW),
		chromedp.Evaluate(layoutWidth, &layoutW),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getAttribute('aria-orientation')`, &orientation),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').tabIndex === 0`, &focusable),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getBoundingClientRect().width`, &hit),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getAttribute('aria-valuenow')`, &nowStart),
		// A drag, by real pointer events at the separator.
		chromedp.ActionFunc(func(ctx context.Context) error {
			return dragSeparator(ctx, splitSeparator, 120)
		}),
		chromedp.Evaluate(sideWidth, &draggedW),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getAttribute('aria-valuenow')`, &nowDragged),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return dragSeparator(ctx, splitSeparator, 2000)
		}),
		chromedp.Evaluate(sideWidth, &wideW),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return dragSeparator(ctx, splitSeparator, -2000)
		}),
		chromedp.Evaluate(sideWidth, &narrowW),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(ready, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(sideWidth, &reloadedW),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').getAttribute('aria-valuenow')`, &nowAfterReload),
		chromedp.Focus(splitSeparator, chromedp.ByQuery),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Evaluate(sideWidth, &afterKey),
		chromedp.KeyEvent(kb.Home),
		chromedp.Evaluate(sideWidth, &resetW),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if role != "separator" || orientation != "vertical" || !focusable {
		t.Errorf("role %q, orientation %q, focusable %v", role, orientation, focusable)
	}
	if hit < 8 {
		t.Errorf("the separator is %.1fpx wide to grab, want at least 8", hit)
	}
	if draggedW <= startW+50 || nowDragged == nowStart {
		t.Errorf("dragging 120px right: list %.0f → %.0fpx, aria-valuenow %s → %s", startW, draggedW, nowStart, nowDragged)
	}
	if wideW > layoutW/2+2 || wideW < layoutW/2-4 {
		t.Errorf("dragged far right: list %.0fpx of %.0fpx, want half", wideW, layoutW)
	}
	if narrowW < 12*rem-1 || narrowW > 12*rem+2 {
		t.Errorf("dragged far left: list %.0fpx, want 12rem (%.0fpx)", narrowW, 12*rem)
	}
	if reloadedW < 12*rem-1 || reloadedW > 12*rem+2 || nowAfterReload == "" {
		t.Errorf("after a reload the list is %.0fpx (valuenow %s), want the remembered 12rem", reloadedW, nowAfterReload)
	}
	if afterKey <= reloadedW+4 {
		t.Errorf("ArrowRight left the list at %.0fpx (was %.0fpx)", afterKey, reloadedW)
	}
	if resetW < startW-2 || resetW > startW+2 {
		t.Errorf("Home reset the list to %.0fpx, want the default %.0fpx", resetW, startW)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// dragSeparator presses the pointer on the separator's centre and moves it by dx px.
func dragSeparator(ctx context.Context, sel string, dx float64) error {
	var box [2]float64
	if err := chromedp.Evaluate(`(() => { const r = document.querySelector('`+sel+`').getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2] })()`, &box).Do(ctx); err != nil {
		return err
	}
	x, y := box[0], box[1]
	if err := input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
		return err
	}
	for i := 1; i <= 8; i++ {
		if err := input.DispatchMouseEvent(input.MouseMoved, x+dx*float64(i)/8, y).WithButton(input.Left).Do(ctx); err != nil {
			return err
		}
	}
	return input.DispatchMouseEvent(input.MouseReleased, x+dx, y).WithButton(input.Left).WithClickCount(1).Do(ctx)
}

// A double-click on the separator gives the default width back.
func TestADoubleClickOnTheCodeSplitResetsItInTheBrowser(t *testing.T) {
	w := runningCodeWorld(t)
	ctx, errs := browser(t)
	sideWidth := `document.querySelector('` + splitSide + `').getBoundingClientRect().width`
	var startW, draggedW, resetW float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(codeTabURL(w)),
		chromedp.Poll(`document.querySelectorAll('[data-ssr]').length === 0 && !!document.querySelector('`+splitSeparator+`')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(sideWidth, &startW),
		chromedp.ActionFunc(func(ctx context.Context) error { return dragSeparator(ctx, splitSeparator, 150) }),
		chromedp.Evaluate(sideWidth, &draggedW),
		chromedp.Evaluate(`document.querySelector('`+splitSeparator+`').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(sideWidth, &resetW),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if draggedW <= startW+50 || resetW < startW-2 || resetW > startW+2 {
		t.Errorf("list %.0f → dragged %.0f → double-clicked %.0fpx", startW, draggedW, resetW)
	}
}
