package web

import (
	"context"
	"fmt"
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
