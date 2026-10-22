package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

const focusedBand = `(document.activeElement?.dataset?.band ?? '')`

// tabTo presses Tab until the band's button has focus, at most 40 times.
func tabTo(band string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for i := 0; i < 40; i++ {
			var at string
			if err := chromedp.Evaluate(focusedBand, &at).Do(ctx); err != nil {
				return err
			}
			if at == band {
				return nil
			}
			if err := chromedp.KeyEvent(kb.Tab).Do(ctx); err != nil {
				return err
			}
		}
		return fmt.Errorf("Tab never reached the %s button", band)
	})
}

// Each summary button is reachable by Tab and toggles on Enter and Space;
// Escape closes the open panel and returns focus to its button.
func TestTheStripWorksFromTheKeyboard(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	ctx, errs := browser(t)

	var closed, opened, escaped, reopened string
	var focusAfterEscape string
	var panelAfterEscape bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		tabTo("feature"),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &closed),
		chromedp.KeyEvent(kb.Enter),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &opened),
		// Escape from inside the panel: focus a row's link first.
		chromedp.Evaluate(`document.querySelector('[data-band-panel="feature"] .row a').focus()`, nil),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!`+panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &escaped),
		chromedp.Evaluate(focusedBand, &focusAfterEscape),
		chromedp.Evaluate(panelOpen(stripPanel), &panelAfterEscape),
		chromedp.KeyEvent(" "),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &reopened),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if closed != "false" || opened != "true" || escaped != "false" || reopened != "true" {
		t.Errorf("aria-expanded: before %q, after Enter %q, after Escape %q, after Space %q", closed, opened, escaped, reopened)
	}
	if focusAfterEscape != "feature" || panelAfterEscape {
		t.Errorf("Escape leaves focus on %q with the panel open %v, want the Feature button with it closed", focusAfterEscape, panelAfterEscape)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The arrow keys, Home and End move focus along the strip without opening
// anything; the agent list keeps its j and k.
func TestTheArrowKeysMoveAlongTheStripAndTheListKeepsItsKeys(t *testing.T) {
	srv, _ := architectServerWith(t, featureChildren(3), nil, pendingReviews(1)...)
	ctx, errs := browser(t)

	var right, end, home, left string
	var anyOpened bool
	var before, after string
	selected := `(document.querySelector('[role=listbox] [role=option][aria-selected=true]:not([data-ssr] *)')?.dataset.sessionId ?? '')`
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(reviewsBtn+`.click()`, nil), // close Reviews, opened by the waiting request
		chromedp.Poll(`!`+panelOpen(reviewsPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(reviewsBtn+`.focus()`, nil),
		chromedp.KeyEvent(kb.ArrowRight),
		chromedp.Evaluate(focusedBand, &right),
		chromedp.KeyEvent(kb.End),
		chromedp.Evaluate(focusedBand, &end),
		chromedp.KeyEvent(kb.Home),
		chromedp.Evaluate(focusedBand, &home),
		chromedp.KeyEvent(kb.ArrowLeft), // already first: stays
		chromedp.Evaluate(focusedBand, &left),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-band-panel]')].some(p => p.checkVisibility())`, &anyOpened),
		chromedp.Evaluate(selected, &before),
		chromedp.Evaluate(`document.querySelector('[role=listbox]').focus()`, nil),
		chromedp.KeyEvent("j"),
		chromedp.Poll(selected+` !== `+strconv.Quote(before), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(selected, &after),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if right != "feature" || end != "feature" || home != "reviews" || left != "reviews" {
		t.Errorf("focus after Right %q, End %q, Home %q, Left %q", right, end, home, left)
	}
	if anyOpened {
		t.Error("moving along the strip opened a panel")
	}
	if before == after {
		t.Errorf("j did not move the agent list (still %q)", after)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A button's accessible name carries its state phrase, the group is named,
// and the open panel is a region its button labels.
func TestTheStripNamesItselfForAScreenReader(t *testing.T) {
	srv, _ := architectServerWith(t, featureChildren(3), nil, pendingReviews(2)...)
	ctx, errs := browser(t)

	var group, reviews, feature, region, scroll, items string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		chromedp.Poll(pageReady+` && `+reviewsBtn+`.getAttribute('aria-label') === 'Reviews, 2 waiting'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=group][aria-label="Task context"]') ? 'Task context' : ''`, &group),
		chromedp.Evaluate(reviewsBtn+`.getAttribute('aria-label')`, &reviews),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-label')`, &feature),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripPanel+`.getAttribute('aria-labelledby') + ':' + `+stripPanel+`.id + ':' + `+stripBtn+`.getAttribute('aria-controls')`, &region),
		chromedp.Evaluate(`document.querySelector('[data-band-panel="feature"] [data-band-scroll]').getAttribute('aria-label') + ':' + document.querySelector('[data-band-panel="feature"] [data-band-scroll]').tabIndex`, &scroll),
		chromedp.Evaluate(`document.querySelectorAll('[data-band-panel="feature"] [role=listitem] a').length + ':' + document.querySelector('[data-band-panel="feature"] [data-band-scroll]').getAttribute('role')`, &items),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if group != "Task context" || reviews != "Reviews, 2 waiting" || feature != "Feature, 3 children" {
		t.Errorf("group %q, Reviews %q, Feature %q", group, reviews, feature)
	}
	if region != "band-feature-button:band-feature:band-feature" {
		t.Errorf("the panel is labelled by its button and controlled by it: %q", region)
	}
	if scroll != "Child tasks, 3:0" {
		t.Errorf("the scroll region is %q, want a named focus stop", scroll)
	}
	if items != "3:list" {
		t.Errorf("the rows are %q, want three links in a list", items)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Below 64rem the strip is still one row with short labels, and the open
// panel keeps the same cap with the agent list on screen.
func TestTheNarrowStripIsOneRowWithShortLabelsAndTheSameCap(t *testing.T) {
	deps := bandedDeps()
	deps.TokenSavers = fakeSaverActivations{}
	url, _ := stripServer(t, deps)
	ctx, errs := browser(t)

	var tops, phrases, shortLabel string
	var panelHeight, innerHeight, listTop, stripWidth, pageWidth float64
	err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(480, 800, 1, true),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Task context"] .cs-btn')].filter(b => b.checkVisibility()).map(b => Math.round(b.getBoundingClientRect().top)).join(',')`, &tops),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Task context"] .phrase')].map(p => getComputedStyle(p).display).join(',')`, &phrases),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('[data-band="token-savers"] .label'), '::after').content`, &shortLabel),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripPanel+`.getBoundingClientRect().height`, &panelHeight),
		chromedp.Evaluate(`window.innerHeight`, &innerHeight),
		chromedp.Evaluate(`document.querySelector('[role=listbox]').getBoundingClientRect().top`, &listTop),
		chromedp.Evaluate(`document.querySelector('[aria-label="Task context"]').getBoundingClientRect().width`, &stripWidth),
		chromedp.Evaluate(`document.documentElement.clientWidth`, &pageWidth),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if len(strings.Split(tops, ",")) != 2 || strings.Split(tops, ",")[0] != strings.Split(tops, ",")[1] {
		t.Errorf("the buttons sit at tops %q, want one row", tops)
	}
	if phrases != "none,none" {
		t.Errorf("the state phrases show on a narrow strip (display %q)", phrases)
	}
	if shortLabel != `"Savers"` {
		t.Errorf("Token savers shortens to %s, want \"Savers\"", shortLabel)
	}
	if limit := innerHeight * 0.3; panelHeight > limit+1 {
		t.Errorf("the panel is %.0fpx high, over the 30svh cap of %.0fpx", panelHeight, limit)
	}
	if listTop+80 > innerHeight {
		t.Errorf("the agent list starts at %.0fpx of a %.0fpx viewport", listTop, innerHeight)
	}
	if stripWidth > pageWidth+1 {
		t.Errorf("the strip is %.0fpx wide in a %.0fpx page: it overflows sideways", stripWidth, pageWidth)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// With reduced motion the panel opens without an animation.
func TestThePanelOpensWithoutAnimationUnderReducedMotion(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	ctx, errs := browser(t)

	var animated, still string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`getComputedStyle(`+stripPanel+`).animationName`, &animated),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
		chromedp.Evaluate(`getComputedStyle(`+stripPanel+`).animationName`, &still),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if animated == "none" || still != "none" {
		t.Errorf("animation name with motion %q, with reduced motion %q", animated, still)
	}
}
