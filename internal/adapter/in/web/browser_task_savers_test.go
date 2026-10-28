package web

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The Edit form's Token savers section is visible without scrolling at 1280
// wide, and on a short narrow screen the form scrolls inside its popover while
// Save stays in reach.
func TestTheEditFormShowsTheTokenSaversAndKeepsSaveInReach(t *testing.T) {
	deps := bandedDeps()
	deps.TokenSavers = fakeSaverActivations{}
	url, _ := stripServer(t, deps)
	ctx, errs := browser(t)

	const section = `document.querySelector('#task-note .task-savers')`
	const form = `document.querySelector('#task-note form')`
	const save = `document.querySelector('#task-note form button[type=submit]')`
	var heading string
	var rows int
	var wideInside bool
	var sectionBottom, formBottom float64
	var saveTop, saveBottom, innerHeight, popTop float64
	var scrolls bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-edit-button').click()`, nil),
		chromedp.Poll(`!!`+section+` && `+section+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(section+`.querySelector('h4').textContent`, &heading),
		chromedp.Evaluate(section+`.querySelectorAll('[data-saver]').length`, &rows),
		chromedp.Evaluate(section+`.getBoundingClientRect().bottom`, &sectionBottom),
		chromedp.Evaluate(form+`.getBoundingClientRect().bottom`, &formBottom),
		chromedp.Evaluate(form+`.scrollHeight <= `+form+`.clientHeight + 1`, &wideInside),
		// A short phone: the form scrolls inside the popover; Save stays on screen.
		chromedp.EmulateViewport(390, 420),
		chromedp.Navigate("about:blank"),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-edit-button').click()`, nil),
		chromedp.Poll(`!!`+section+` && `+section+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`Math.round(document.getElementById('task-note').getBoundingClientRect().bottom) === window.innerHeight`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-note').getBoundingClientRect().top`, &popTop),
		chromedp.Evaluate(form+`.scrollHeight > `+form+`.clientHeight`, &scrolls),
		chromedp.Evaluate(save+`.getBoundingClientRect().top`, &saveTop),
		chromedp.Evaluate(save+`.getBoundingClientRect().bottom`, &saveBottom),
		chromedp.Evaluate(`window.innerHeight`, &innerHeight),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if heading != "Token savers" || rows != 2 {
		t.Errorf("the section is %q with %d savers, want Token savers with the 2 the server gave", heading, rows)
	}
	if !wideInside || sectionBottom > formBottom {
		t.Errorf("at 1280 wide the section is visible without scrolling: form fits %v, section bottom %.0f of %.0f", wideInside, sectionBottom, formBottom)
	}
	if saveTop < popTop || saveBottom > innerHeight+1 {
		t.Errorf("on a short phone Save is out of reach: %.0f to %.0f in a screen of %.0f (popover top %.0f); the form scrolls: %v", saveTop, saveBottom, innerHeight, popTop, scrolls)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// With no token savers served the Edit form has no such section.
func TestTheEditFormHasNoTokenSaversSectionWhenNoneAreServed(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	ctx, errs := browser(t)
	var present bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('task-edit-button').click()`, nil),
		chromedp.Poll(`document.getElementById('task-note').matches(':popover-open')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#task-note .task-savers'); return !!s && s.checkVisibility(); })()`, &present),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if present {
		t.Error("a Token savers section without token savers")
	}
}
