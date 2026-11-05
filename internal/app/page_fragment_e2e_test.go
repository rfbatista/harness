//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// navRequest is what htmx sends for a boosted click from the page at from.
func navRequest(path, from string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("HX-Request", "true")
	r.Header.Set("HX-Boosted", "true")
	r.Header.Set("HX-Target", "app-shell")
	if from != "" {
		r.Header.Set("HX-Current-URL", "http://127.0.0.1:8080"+from)
	}
	return r
}

// A click between two pages of one project is answered with the page
// fragment: <main> with its seed and the out-of-band items, built without the
// shared project frame (ADR 0064).
func TestAPageFragmentIsMainWithoutTheSharedProjectFrame(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(6)
	base := "/projects/" + b.project
	pages := map[string]string{
		"board":     base,
		"task page": base + "/tasks/" + b.tickets[0],
	}
	for name, path := range pages {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			b.handler.ServeHTTP(rec, navRequest(path, base+"/settings"))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", path, rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `<main id="main"`) {
				t.Errorf("fragment has no <main>:\n%.400s", body)
			}
			for _, absent := range []string{"<html", `class="[ topbar ]"`, `id="rail"`, "rail-seed", "app-shell"} {
				if strings.Contains(body, absent) {
					t.Errorf("fragment holds the frame (%q)", absent)
				}
			}
			for _, oob := range []string{"<title>", `id="frame-state"`, `id="stream-bar"`} {
				if !strings.Contains(body, oob) {
					t.Errorf("fragment lacks the out-of-band %s", oob)
				}
			}
			if got := strings.Count(body, "hx-swap-oob"); got != 2 {
				t.Errorf("want 2 out-of-band items (frame state, stream bar), got %d", got)
			}
			h := rec.Header()
			if h.Get("X-Harness-Fragment") != "page" || h.Get("HX-Retarget") != "#main" || h.Get("HX-Reswap") != "outerHTML" {
				t.Errorf("fragment is not marked and retargeted: %v", h)
			}
			vary := strings.Join(h.Values("Vary"), ",")
			if !strings.Contains(vary, "HX-Request") || !strings.Contains(vary, "HX-Current-URL") {
				t.Errorf("Vary = %q, want HX-Request and HX-Current-URL", vary)
			}
			if m := parseServerTiming(t, h.Get("Server-Timing")); m["frame"] != 0 {
				t.Errorf("a fragment builds no frame, Server-Timing frame = %v", m["frame"])
			}
		})
	}
}

// The same URL without HX-Request is the full page, with the frame and no
// fragment marking.
func TestThePageURLWithoutHXRequestIsStillTheFullPage(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(2)
	base := "/projects/" + b.project
	for _, path := range []string{base, base + "/tasks/" + b.tickets[0]} {
		rec := httptest.NewRecorder()
		b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		for _, want := range []string{"<html", `class="[ topbar ]"`, `id="rail"`, `<main id="main"`, `id="stream-bar"`, `id="frame-state"`, `id="app-shell"`, `hx-target="#app-shell"`, `hx-push-url="true"`} {
			if !strings.Contains(body, want) {
				t.Errorf("full page %s lacks %q", path, want)
			}
		}
		if strings.Contains(body, "hx-swap-oob") || rec.Header().Get("X-Harness-Fragment") != "" {
			t.Errorf("full page %s is marked as a fragment", path)
		}
		vary := strings.Join(rec.Header().Values("Vary"), ",")
		if !strings.Contains(vary, "HX-Request") || !strings.Contains(vary, "HX-Current-URL") {
			t.Errorf("Vary = %q on the full page", vary)
		}
	}
}

// A fragment of a page that does not exist is the same 404 as the full page.
func TestAFragmentOfAMissingTaskIsTheSame404(t *testing.T) {
	b := newBudgetProject(t)
	base := "/projects/" + b.project
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, navRequest(base+"/tasks/nope", base))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
