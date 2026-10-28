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

// The Feature button the server paints and the one the live page draws sit in
// the same place: the handover does not move the agents.
func TestTheFeatureButtonDoesNotJumpWhenTheLivePageTakesOver(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	geometry := `(() => { const b = ` + featureBtn + `.getBoundingClientRect(), l = document.querySelector('[role=listbox]').getBoundingClientRect(); return [b.top, b.left, b.width, b.height, l.top].map(Math.round).join(',') })()`

	// The server's paint: scripts off.
	paintedCtx, paintedErrs := browser(t)
	var painted, label string
	err := chromedp.Run(paintedCtx,
		emulation.SetScriptExecutionDisabled(true),
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(url+"/projects/p1/tasks/t-feed"),
		waitForFonts,
		chromedp.Evaluate(geometry, &painted),
		chromedp.Evaluate(featureBtn+`.textContent.replace(/\s+/g, ' ').trim()`, &label),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, paintedErrs.all())
	}
	if label != "Feature 16" {
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
		t.Errorf("the Feature button and the agent list moved at the handover: painted %s, live %s", painted, live)
	}
}

const (
	reviewsBtn   = `document.querySelector('[data-band="reviews"]')`
	reviewsPanel = `document.querySelector('[data-band-panel="reviews"]')`
)

// pendingReviews are n requests waiting on the person, the first the oldest.
func pendingReviews(n int) []*domain.ReviewRequest {
	now := time.Now()
	var out []*domain.ReviewRequest
	for i := 0; i < n; i++ {
		at := now.Add(-time.Duration(n-i) * time.Minute)
		out = append(out, &domain.ReviewRequest{ID: fmt.Sprintf("rv%d", i+1), TaskID: "t1", ProjectID: "p1", ArchitectSessionID: "s-arch", AboutSessionID: "s-server",
			Subject: fmt.Sprintf("Review number %d", i+1), Body: "Three specs and two contracts.\nPlease check the status authority rule.", DocumentIDs: []string{}, ArtifactIDs: []string{},
			State: domain.ReviewPending, CreatedAt: at, UpdatedAt: at})
	}
	return out
}

func featureChildren(n int) []*domain.Ticket {
	var out []*domain.Ticket
	for i := 0; i < n; i++ {
		out = append(out, &domain.Ticket{ID: fmt.Sprintf("c%02d", i), ProjectID: "p1", Title: fmt.Sprintf("Child task %d", i), Status: domain.TicketStatusTodo, ParentID: "t1"})
	}
	return out
}

type fakeSaverActivations struct{}

func (fakeSaverActivations) ListTokenSaverActivations(context.Context, domain.ActivationOwner) ([]domain.TokenSaverActivation, error) {
	return []domain.TokenSaverActivation{
		{Saver: domain.TokenSaverRTK, Value: domain.ActivationInherit, Summary: "inherit — resolves off (project)"},
		{Saver: domain.TokenSaverCodegraph, Value: domain.ActivationOn, Summary: "on — resolves on (task)"},
	}, nil
}

const bandCookie = `(document.cookie.split('; ').find(c => c.startsWith('task_band=')) ?? '').slice('task_band='.length)`

func panelOpen(sel string) string { return `(` + sel + `.checkVisibility())` }

// The panel left open is remembered: after a reload the server's paint
// already has it open (scripts off), and so does the live page.
func TestTheOpenPanelSurvivesAReloadAndTheServerPaintsItOpen(t *testing.T) {
	url, _ := stripServer(t, bandedDeps())
	ctx, errs := browser(t)

	page := url + "/projects/p1/tasks/t-feed"
	var cookie string
	var painted, paintedExpanded, live bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(page),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(stripBtn+`.click()`, nil),
		chromedp.Poll(panelOpen(stripPanel), nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(bandCookie, &cookie),
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(page),
		chromedp.Evaluate(panelOpen(stripPanel), &painted),
		chromedp.Evaluate(stripBtn+`.getAttribute('aria-expanded') === 'true'`, &paintedExpanded),
		emulation.SetScriptExecutionDisabled(false),
		chromedp.Navigate(page),
		chromedp.Poll(pageReady, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(panelOpen(stripPanel), &live),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if cookie != "feature" {
		t.Errorf("task_band = %q after opening Feature, want feature", cookie)
	}
	if !painted || !paintedExpanded {
		t.Errorf("the server's paint has Feature open: panel %v, aria-expanded %v", painted, paintedExpanded)
	}
	if !live {
		t.Error("the live page lost the open panel")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
