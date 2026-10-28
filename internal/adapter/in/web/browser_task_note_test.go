package web

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"operators-mcp/internal/domain"
)

const (
	noteButton = `document.querySelector('#task-note-button')`
	notePopup  = `document.querySelector('#task-note')`
	// The agents' area: the session list's box, which the page fixes to the viewport.
	agentsBox = `(() => { const r = document.querySelector('.split-view > [role=listbox]').getBoundingClientRect(); return [r.top, r.height].map(Math.round).join(',') })()`
)

// noteDeps is a task whose page has a session open (boardDeps's).
func noteDeps(description string) Deps {
	deps := boardDeps()
	deps.Tasks = fakeTickets{[]*domain.Ticket{
		{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Description: description, Status: domain.TicketStatusInProgress},
	}}
	return deps
}

// The task note is a popover from the title row: with a long description the
// agents' area keeps its place and height on load and while the note is open;
// the popover shows the whole text; Esc closes it and the button has the
// focus again; a reload starts closed.
func TestTheTaskNoteOpensAsAPopoverThatMovesNothing(t *testing.T) {
	long := strings.Repeat("Line of the description that the person wrote.\n", 12)
	url, _ := pageServer(t, noteDeps(long))
	ctx, errs := browser(t)

	var before, open, expanded, text, focused, closed, reloaded string
	var openAfterReload bool
	var popupHeight, viewport float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && `+noteButton+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(agentsBox, &before),
		chromedp.Evaluate(noteButton+`.click()`, nil),
		chromedp.Poll(notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(agentsBox, &open),
		chromedp.Evaluate(noteButton+`.getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(notePopup+`.querySelector('.note-body').textContent`, &text),
		chromedp.Evaluate(notePopup+`.getBoundingClientRect().height`, &popupHeight),
		chromedp.Evaluate(`innerHeight`, &viewport),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!`+notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.activeElement === `+noteButton+` ? 'button' : document.activeElement.tagName`, &focused),
		chromedp.Evaluate(noteButton+`.getAttribute('aria-expanded')`, &closed),
		chromedp.Evaluate(noteButton+`.click()`, nil),
		chromedp.Poll(notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && `+noteButton+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(notePopup+`.matches(':popover-open')`, &openAfterReload),
		chromedp.Evaluate(agentsBox, &reloaded),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if open != before || reloaded != before {
		t.Errorf("the agents' area moved: before %s, with the note open %s, after a reload %s", before, open, reloaded)
	}
	if expanded != "true" || closed != "false" {
		t.Errorf("aria-expanded: open %q, closed %q", expanded, closed)
	}
	if text != long[:len(long)-1] && strings.TrimSpace(text) != strings.TrimSpace(long) {
		t.Errorf("the popover shows %q, want the whole description", text)
	}
	if popupHeight > 40*16 || popupHeight > viewport*0.7+1 {
		t.Errorf("the popover is %.0fpx high in a %.0fpx viewport: want at most min(70dvh, 40rem)", popupHeight, viewport)
	}
	if focused != "button" {
		t.Errorf("after Esc the focus is on %s, want the Description button", focused)
	}
	if openAfterReload {
		t.Error("a reload opened the note: its state must never be stored")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Edit shows the form in the same surface; the agents' area stays put; Save
// closes it and the Description button then opens the new text.
func TestTheTaskNoteEditsInTheSameSurface(t *testing.T) {
	url, _ := pageServer(t, noteDeps("The first words."))
	ctx, errs := browser(t)

	var before, editing string
	var formInPopup bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady+` && `+noteButton+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(agentsBox, &before),
		chromedp.Evaluate(noteButton+`.click()`, nil),
		chromedp.Poll(notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...`+notePopup+`.querySelectorAll('button')].find(b => b.textContent.trim() === 'Edit').click()`, nil),
		chromedp.Poll(`!!document.querySelector('#task-note #task-edit-description')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`!!`+notePopup+`.querySelector('#task-edit-title') && `+notePopup+`.matches(':popover-open')`, &formInPopup),
		chromedp.Evaluate(agentsBox, &editing),
		chromedp.Evaluate(`[...`+notePopup+`.querySelectorAll('button')].find(b => b.textContent.trim() === 'Cancel').click()`, nil),
		chromedp.Poll(`!`+notePopup+`.querySelector('#task-edit-description') && `+notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !formInPopup || editing != before {
		t.Errorf("form in the popover %v; agents' area before %s, while editing %s", formInPopup, before, editing)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A task with no description has no Description button; the title row's Edit
// opens the form in the surface.
func TestATaskWithoutADescriptionHasNoButtonAndEditsFromTheTitleRow(t *testing.T) {
	url, _ := pageServer(t, noteDeps(""))
	ctx, errs := browser(t)

	var buttonShown, formOpen bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(noteButton+`.checkVisibility()`, &buttonShown),
		chromedp.Evaluate(`document.querySelector('#task-edit-button').click()`, nil),
		chromedp.Poll(`!!`+notePopup+`.querySelector('#task-edit-title')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(notePopup+`.matches(':popover-open')`, &formOpen),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if buttonShown || !formOpen {
		t.Errorf("Description button shown %v, form open %v", buttonShown, formOpen)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The popover hangs from the Description button, as the Checks panel does from
// its button: its left edge under the button, or its right edge against the
// button's when it would run off the viewport; always inside the viewport,
// also when the toolbar wraps.
func TestTheTaskNoteHangsFromTheDescriptionButton(t *testing.T) {
	url, _ := pageServer(t, noteDeps(strings.Repeat("Line of the description that the person wrote.\n", 12)))
	ctx, errs := browser(t)

	for _, width := range []int64{1680, 1280, 1000, 800} {
		var geometry string
		err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
			chromedp.Poll(pageReady+` && `+noteButton+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(10*time.Second)),
			chromedp.Evaluate(noteButton+`.click()`, nil),
			chromedp.Poll(notePopup+`.matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`(() => { const b = `+noteButton+`.getBoundingClientRect(), p = `+notePopup+`.getBoundingClientRect();
				const leftUnder = Math.abs(p.left - b.left) <= 2, rightAgainst = Math.abs(p.right - b.right) <= 2;
				const clamped = p.left <= 9 || p.right >= innerWidth - 9 || p.left <= b.left && p.right >= b.right;
				return JSON.stringify({ ok: (leftUnder || rightAgainst || clamped) && p.top >= b.bottom && p.left >= 0 && p.right <= innerWidth, b: [b.left, b.right, b.bottom], p: [p.left, p.right, p.top] }) })()`, &geometry),
		)
		if err != nil {
			t.Fatalf("%dpx: %v\nJS errors: %v", width, err, errs.all())
		}
		if !strings.Contains(geometry, `"ok":true`) {
			t.Errorf("at %dpx the popover does not hang from the button: %s", width, geometry)
		}
	}
}
