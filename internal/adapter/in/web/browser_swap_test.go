package web

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/chromedp/chromedp"
)

// failingSwaps answers 500 to every htmx request while fail is set, and
// passes everything else (full page loads included) to h: a swap that fails
// on the server.
func failingSwaps(h http.Handler) (http.Handler, *atomic.Bool) {
	fail := new(atomic.Bool)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() && r.Header.Get("HX-Request") == "true" {
			http.Error(w, "swap failed", http.StatusInternalServerError)
			return
		}
		h.ServeHTTP(w, r)
	}), fail
}

// markWindow sets a marker on window that only a full page load clears.
func markWindow() chromedp.Action {
	return chromedp.Evaluate(`window.__swapMarker = true`, nil)
}

// windowMarked reads whether the marker survived: true after a swap in
// place, false after a full page load.
func windowMarked(got *bool) chromedp.Action {
	return chromedp.Evaluate(`window.__swapMarker === true`, got)
}

// pageSettled waits for a full page load to finish, htmx and Alpine started.
func pageSettled() chromedp.Action {
	return chromedp.Poll(`document.readyState === 'complete'`, nil, chromedp.WithPollingTimeout(10*time.Second))
}

// pollAcrossLoads waits until expr is true, through the full page load a
// history step or a failed swap starts: chromedp.Poll gives up when the page
// it polls navigates away.
func pollAcrossLoads(expr string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			var ok bool
			if err := chromedp.Evaluate(expr, &ok).Do(ctx); err == nil && ok {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
		return fmt.Errorf("still not %s", expr)
	})
}
