package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
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

// swapServer serves the documents pages of world w with failingSwaps in
// front; /api is not under test.
func swapServer(t *testing.T, w world, assets *Assets) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	pages, fail := failingSwaps(NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env, Documents: docReader{w.docs, w.tickets},
		Now: func() time.Time { return now },
	}, assets, nil))
	mux := http.NewServeMux()
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, fail
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

// TestASwapNeverRunsAScriptInTheBrowser: a fragment holding a script that
// could run (no type, a JavaScript type, or a module) is never inserted: the
// script does not run, and the URL loads as a page instead. The script would
// leave its mark in sessionStorage, which outlives that load.
func TestASwapNeverRunsAScriptInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	srv, _ := swapServer(t, documentsBoard(), assets)
	pages := srv.Config.Handler
	var script atomic.Value
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("HX-Request") != "true" {
			pages.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		pages.ServeHTTP(rec, r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, strings.Replace(rec.Body.String(), "</nav>", script.Load().(string)+"</nav>", 1))
	})
	base := srv.URL + "/projects/p1/tasks/t-feed/documents"

	for _, tag := range []string{`<script>`, `<script type="">`, `<script type="text/javascript">`, `<script type="module">`, `<script type=" Application/JavaScript ">`} {
		script.Store(tag + `sessionStorage.setItem('ran', ` + strconv.Quote(tag) + `)</script>`)
		var ran string
		var reloaded bool
		err := chromedp.Run(ctx,
			chromedp.Navigate(base),
			pageSettled(),
			chromedp.Evaluate(`sessionStorage.clear(); true`, nil),
			markWindow(),
			chromedp.Evaluate(`document.querySelector('nav[aria-label="Documents of this task"] a[href$="/documents/d-plan"]').click()`, nil),
			pollAcrossLoads(`document.readyState === 'complete' && location.pathname.endsWith('/documents/d-plan') && !!document.querySelector('article .prose table')`),
			windowMarked(&reloaded),
			chromedp.Evaluate(`sessionStorage.getItem('ran') ?? ''`, &ran),
		)
		if err != nil {
			t.Fatalf("%s: %v\nJS errors: %v", tag, err, errs.all())
		}
		if ran != "" {
			t.Errorf("%s: the swapped-in script ran", tag)
		}
		if reloaded {
			t.Errorf("%s: the fragment was swapped in instead of the page loading", tag)
		}
	}
}
