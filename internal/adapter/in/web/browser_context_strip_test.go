package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// bandedWorld is a feature task with sixteen children (3 in review, 2 in
// progress, 11 todo) and one session: the page whose bands used to fill the
// screen.
func bandedDeps() Deps {
	deps := boardDeps()
	tickets := []*domain.Ticket{{ID: "t-feed", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}
	for i := 0; i < 16; i++ {
		status := domain.TicketStatusTodo
		switch {
		case i < 3:
			status = domain.TicketStatusReview
		case i < 5:
			status = domain.TicketStatusInProgress
		}
		tickets = append(tickets, &domain.Ticket{ID: fmt.Sprintf("c%02d", i), ProjectID: "p1", Title: fmt.Sprintf("Child task %d", i), Status: status, ParentID: "t-feed"})
	}
	deps.Tasks = fakeTickets{tickets}
	deps.Repositories = fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}}
	return deps
}

// waitForFonts waits, with scripts off (Poll would hang), until the webfonts
// are in: text measures differently before they arrive.
var waitForFonts = chromedp.ActionFunc(func(ctx context.Context) error {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		var loaded bool
		if err := chromedp.Evaluate(fontsLoaded, &loaded).Do(ctx); err != nil || loaded {
			return err
		}
	}
	return fmt.Errorf("the webfonts did not load")
})

func stripServer(t *testing.T, deps Deps) (string, *sseFeed) {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(deps, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv.URL, feed
}

const (
	stripBtn   = `document.querySelector('[data-band="feature"]')`
	stripPanel = `document.querySelector('[data-band-panel="feature"]')`
	pageReady  = `document.querySelector('[data-ssr]') === null`
	// Text measures differently before the webfonts arrive.
	fontsLoaded = `document.fonts.status === 'loaded'`
)

// A feature task with sixteen children shows one strip button that says
// where the feature stands; opening it shows a panel that stops at the cap
// and scrolls inside, and the agents stay on screen.
func TestTheContextStripKeepsTheAgentsOnScreenWithSixteenChildren(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	ctx, errs := browser(t)

	var label, closedExpanded, openExpanded, afterExpanded string
	var panelClosed, panelOpen, panelAfter bool
	var panelHeight, clientHeight, scrollHeight, listTop, innerHeight float64
	var rows int
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=group][aria-label="Task context"] [data-band="feature"]').textContent.replace(/\s+/g, ' ').trim()`, &label),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &closedExpanded),
		chromedp.Evaluate(stripPanel+`.checkVisibility()`, &panelClosed),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(stripPanel+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &openExpanded),
		chromedp.Evaluate(stripPanel+`.checkVisibility()`, &panelOpen),
		chromedp.Evaluate(`document.querySelectorAll('[data-band-panel="feature"] .row[data-child-id]').length`, &rows),
		chromedp.Evaluate(stripPanel+`.getBoundingClientRect().height`, &panelHeight),
		chromedp.Evaluate(`document.querySelector('[data-band-panel="feature"] [data-band-scroll]').clientHeight`, &clientHeight),
		chromedp.Evaluate(`document.querySelector('[data-band-panel="feature"] [data-band-scroll]').scrollHeight`, &scrollHeight),
		chromedp.Evaluate(`document.querySelector('[role=listbox]').getBoundingClientRect().top`, &listTop),
		chromedp.Evaluate(`window.innerHeight`, &innerHeight),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(`!`+stripPanel+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded')`, &afterExpanded),
		chromedp.Evaluate(stripPanel+`.checkVisibility()`, &panelAfter),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if label != "Feature 16 3 in review · 2 in progress" {
		t.Errorf("the Feature button reads %q", label)
	}
	if panelClosed || closedExpanded != "false" {
		t.Errorf("the panel starts closed: visible %v, aria-expanded %q", panelClosed, closedExpanded)
	}
	if !panelOpen || openExpanded != "true" || rows != 16 {
		t.Errorf("opened: visible %v, aria-expanded %q, %d rows (want 16)", panelOpen, openExpanded, rows)
	}
	if limit := innerHeight * 0.3; panelHeight > limit+1 {
		t.Errorf("the panel is %.0fpx high, over the 30svh cap of %.0fpx", panelHeight, limit)
	}
	if scrollHeight <= clientHeight {
		t.Errorf("16 rows must scroll inside the panel: scrollHeight %.0f, clientHeight %.0f", scrollHeight, clientHeight)
	}
	if listTop+120 > innerHeight {
		t.Errorf("the agent list starts at %.0fpx of a %.0fpx viewport: it is off screen", listTop, innerHeight)
	}
	if panelAfter || afterExpanded != "false" {
		t.Errorf("a second click closes it: visible %v, aria-expanded %q", panelAfter, afterExpanded)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// A task with no children, no reviews and no token savers draws no strip.
func TestATaskWithNothingToSummariseDrawsNoContextStrip(t *testing.T) {
	url, _ := stripServer(t, boardDeps())
	ctx, errs := browser(t)

	var drawn bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('[aria-label="Task context"]')].some(e => e.checkVisibility())`, &drawn),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if drawn {
		t.Error("the strip is drawn with nothing to show")
	}
}

// The strip the server paints and the strip the live page draws sit in the
// same place: the handover does not move the agents.
func TestTheContextStripDoesNotJumpWhenTheLivePageTakesOver(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	geometry := `(() => { const b = ` + stripBtn + `.getBoundingClientRect(), l = document.querySelector('[role=listbox]').getBoundingClientRect(); return [b.top, b.left, b.width, b.height, l.top].map(Math.round).join(',') })()`

	// The server's paint: scripts off.
	paintedCtx, paintedErrs := browser(t)
	var painted, label string
	err := chromedp.Run(paintedCtx,
		emulation.SetScriptExecutionDisabled(true),
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		waitForFonts,
		chromedp.Evaluate(geometry, &painted),
		chromedp.Evaluate(stripBtn+`.textContent.replace(/\s+/g, ' ').trim()`, &label),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, paintedErrs.all())
	}
	if label != "Feature 16 3 in review · 2 in progress" {
		t.Errorf("the server paints the Feature button as %q", label)
	}

	liveCtx, liveErrs := browser(t)
	var live string
	err = chromedp.Run(liveCtx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Poll(fontsLoaded, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(geometry, &live),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, liveErrs.all())
	}
	if painted != live {
		t.Errorf("the strip and the agent list moved at the handover: painted %s, live %s", painted, live)
	}
}

// Children move and arrive over the feed: the count, the phrase and the row
// order follow, and the open panel keeps its scroll position.
func TestTheContextStripFollowsTheChildrenLive(t *testing.T) {
	url, feed := stripServer(t, bandedDeps())
	ctx, errs := browser(t)

	label := `document.querySelector('[data-band="feature"]').textContent.replace(/\s+/g, ' ').trim()`
	order := `[...document.querySelectorAll('[data-band-panel="feature"] .row[data-child-id]')].map(r => r.dataset.childId).slice(0, 4).join(',')`
	var before, after, orderBefore, orderAfter string
	var scrolled float64
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(stripPanel+`.checkVisibility()`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(label, &before),
		chromedp.Evaluate(order, &orderBefore),
		chromedp.Evaluate(`(() => { const s = document.querySelector('[data-band-panel="feature"] [data-band-scroll]'); s.scrollTop = 60; return s.scrollTop })()`, &scrolled),
		chromedp.ActionFunc(func(context.Context) error {
			for feed.followers() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			// c10 (todo) goes to review; a new child arrives in backlog.
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "c10", "project_id": "p1", "title": "Child task 10", "status": "review", "parent_id": "t-feed"}})
			feed.pushRaw(t, map[string]any{"ticket": map[string]any{"id": "t-new", "project_id": "p1", "title": "New one", "status": "backlog", "parent_id": "t-feed"}})
			return nil
		}),
		chromedp.Poll(`(`+label+`).startsWith('Feature 17 4 in review')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(label, &after),
		chromedp.Evaluate(order, &orderAfter),
		chromedp.Evaluate(`document.querySelector('[data-band-panel="feature"] [data-band-scroll]').scrollTop`, &scrolled),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if before != "Feature 16 3 in review · 2 in progress" || after != "Feature 17 4 in review · 2 in progress" {
		t.Errorf("button before %q, after %q", before, after)
	}
	if orderBefore != "c00,c01,c02,c03" || orderAfter != "c00,c01,c02,c10" {
		t.Errorf("row order before %q, after %q", orderBefore, orderAfter)
	}
	if scrolled != 60 {
		t.Errorf("a live change moved the open panel's scroll to %.0f, want it to stay at 60", scrolled)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
