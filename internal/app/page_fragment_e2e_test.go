//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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
		for _, want := range []string{"<html", `class="[ topbar ]"`, `id="rail"`, `<main id="main"`, `id="stream-bar"`, `id="frame-state"`, `id="app-shell"`, `hx-target="#app-shell"`} {
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

// A fragment that the browser asked for ahead of the click (hover or focus on
// a link) may be held for the click that follows, for 5 s, and no longer
// (ADR 0064, ADR 0062). Any other answer is never stored.
func TestOnlyAPrefetchedPageIsHeldBriefly(t *testing.T) {
	b := newBudgetProject(t)
	base := "/projects/" + b.project

	plain := httptest.NewRecorder()
	b.handler.ServeHTTP(plain, navRequest(base+"/reviews", base))
	if got := plain.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a click's answer: Cache-Control = %q, want no-store", got)
	}

	ahead := navRequest(base+"/reviews", base)
	ahead.Header.Set("X-Harness-Prefetch", "1")
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, ahead)
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=5" {
		t.Errorf("a prefetch's answer: Cache-Control = %q, want private, max-age=5", got)
	}
	if vary := strings.Join(rec.Header().Values("Vary"), ","); !strings.Contains(vary, "X-Harness-Prefetch") {
		t.Errorf("Vary = %q: a prefetched answer must be used only by the click that asks for it", vary)
	}

	missing := navRequest(base+"/tasks/nope", base)
	missing.Header.Set("X-Harness-Prefetch", "1")
	rec = httptest.NewRecorder()
	b.handler.ServeHTTP(rec, missing)
	if rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a prefetched error: status %d, Cache-Control %q; want 404 and no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}

	full := httptest.NewRequest(http.MethodGet, base+"/reviews", nil)
	full.Header.Set("X-Harness-Prefetch", "1")
	rec = httptest.NewRecorder()
	b.handler.ServeHTTP(rec, full)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("a full page is never held, Cache-Control = %q", got)
	}
}

// The task page's worktree git state costs a git call per worktree, so a
// page fragment leaves it out and the browser asks for it after the page, at
// the page's own URL, naming the section (ADR 0064). The full page keeps
// it, as on main.
func TestTheTaskPageFragmentLoadsItsGitStateAfterThePage(t *testing.T) {
	b := newBudgetProject(t)
	b.live = 3
	b.addSessions(3)
	page := "/projects/" + b.project + "/tasks/" + b.tickets[0]
	gitStates := regexp.MustCompile(`"git_states":\[\{`)

	frag := httptest.NewRecorder()
	b.handler.ServeHTTP(frag, navRequest(page, "/projects/"+b.project))
	body := frag.Body.String()
	if gitStates.MatchString(body) {
		t.Errorf("the fragment carries git states: they load after the page")
	}
	for _, want := range []string{`id="git-states"`, `hx-get="` + page + `"`, `hx-trigger="load"`, `"git_states":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("the fragment lacks %q", want)
		}
	}
	if m := parseServerTiming(t, frag.Header().Get("Server-Timing")); m["git"] != 0 {
		t.Errorf("the fragment spent %v on git", m["git"])
	}

	full := httptest.NewRecorder()
	b.handler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, page, nil))
	if !gitStates.MatchString(full.Body.String()) || strings.Contains(full.Body.String(), `id="git-states"`) {
		t.Errorf("the full page must carry its git states and no lazy section")
	}

	section := httptest.NewRequest(http.MethodGet, page, nil)
	section.Header.Set("HX-Request", "true")
	section.Header.Set("HX-Target", "git-states")
	sec := httptest.NewRecorder()
	b.handler.ServeHTTP(sec, section)
	sb := sec.Body.String()
	if sec.Code != http.StatusOK || !gitStates.MatchString(sb) || !strings.Contains(sb, `id="git-states-seed"`) {
		t.Fatalf("section: status %d, body %.300s", sec.Code, sb)
	}
	for _, absent := range []string{"<main", "<html", "hx-swap-oob"} {
		if strings.Contains(sb, absent) {
			t.Errorf("the section holds %q: it is the section alone", absent)
		}
	}
	if sec.Header().Get("X-Harness-Fragment") != "" || sec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a section is no page fragment and is never held: %v", sec.Header())
	}

	unknown := httptest.NewRequest(http.MethodGet, page, nil)
	unknown.Header.Set("HX-Request", "true")
	unknown.Header.Set("HX-Target", "nope")
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, unknown)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), `<main id="main" class="[ frame ]" x-data="sessionsPage"`) {
		t.Errorf("an unknown section: status %d; it must be a not-found, never the whole page", rec.Code)
	}
}
