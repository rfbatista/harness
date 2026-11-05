package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// fragmentWorld is the board with what the pages that need more than tasks
// read: documents, design assets and a design system.
func fragmentWorld() world {
	w := documentsBoard()
	w.docs["t-docs"] = []*domain.Document{{ID: "d-other", ProjectID: "p1", Title: "Other"}}
	w.artifacts = designBoard().artifacts
	w.designs = savedDesignSystem()
	w.summaries = fakeSummaries{{Project: w.projects[0]}, {Project: w.projects[1]}}
	return w
}

// A page and where the browser comes from when it is clicked: another page of
// the same frame.
type fragmentPage struct{ path, from string }

const (
	inProject = "/projects/p1/documents"
	outside   = "/notifications"
)

// pages names each page route the shell serves and one URL of it, keyed by the
// route as web.go registers it.
var fragmentPages = map[string]fragmentPage{
	`/projects/{project}`:                                   {"/projects/p1", inProject},
	`/projects/{project}/tasks/{task}`:                      {"/projects/p1/tasks/t-feed", inProject},
	`/projects/{project}/tasks/new`:                         {"/projects/p1/tasks/new", inProject},
	`/projects/{project}/archived`:                          {"/projects/p1/archived", inProject},
	`/projects/{project}/reviews`:                           {"/projects/p1/reviews", inProject},
	`/projects/{project}/sessions`:                          {"/projects/p1/sessions", inProject},
	`/projects/{project}/design`:                            {"/projects/p1/design", inProject},
	`/projects/{project}/tasks/{task}/design`:               {"/projects/p1/tasks/t-feed/design", inProject},
	`/projects/{project}/tasks/{task}/resources`:            {"/projects/p1/tasks/t-feed/resources", inProject},
	`/projects/{project}/documents`:                         {"/projects/p1/documents", "/projects/p1"},
	`/projects/{project}/documents/{document}`:              {"/projects/p1/documents/d-page", inProject},
	`/projects/{project}/tasks/{task}/documents`:            {"/projects/p1/tasks/t-feed/documents", inProject},
	`/projects/{project}/tasks/{task}/documents/{document}`: {"/projects/p1/tasks/t-feed/documents/d-plan", inProject},
	`/projects/{project}/design-systems`:                    {"/projects/p1/design-systems", inProject},
	`/projects/{project}/design-systems/{slug}`:             {"/projects/p1/design-systems/app", inProject},
	`/projects/{project}/settings`:                          {"/projects/p1/settings", inProject},
	`/projects/{project}/repositories`:                      {"/projects/p1/repositories", inProject},
	`/projects/{project}/repositories/{repository}/env`:     {"/projects/p1/repositories/r-harness/env", inProject},
	`/projects`:     {"/projects", outside},
	`/projects/new`: {"/projects/new", outside},
}

// notInTheTable are page routes this table cannot try: the ones that need
// dependencies the test world does not wire (settings stores, the sandbox, the
// code browser, workspaces, the agent catalog), which go through the same
// shared way as the others (page and AppShell); the home redirect; and the
// sandboxed document views, which answer a document and not a page.
var notInTheTable = []string{
	`/{$}`, `/projects/{project}/documents/{document}/view`, `/projects/{project}/tasks/{task}/documents/{document}/view`,
	`notifications.Href`, `credentials.Href`, `tokensavers.Href`, `agents.Href`, `models.Href`, `agentisolation.Href`, `serversettings.Href`,
	`/projects/{project}/usage`, `/projects/{project}/code`,
	`/projects/{project}/repositories/{repository}/code`,
	`/projects/{project}/repositories/{repository}/history`,
	`/projects/{project}/tasks/{task}/sessions/{session}/history`,
	`/projects/{project}/repositories/{repository}/architecture`,
	`/projects/{project}/repositories/{repository}/history/{commit}`,
	`/projects/{project}/repositories/{repository}/code/files`, `/projects/{project}/repositories/{repository}/code/files/{path...}`, `/projects/{project}/repositories/{repository}/code/map`,
	`/projects/{project}/tasks/{task}/sessions/{session}/history/{commit}`,
}

// Every URL answers two shapes (ADR 0064): with HX-Request, from a page of
// its own frame, the page fragment; without it, the full page.
func TestEveryPageAnswersItsFragmentAndItsFullPage(t *testing.T) {
	h := newTestHandler(t, fragmentWorld())
	for route, page := range fragmentPages {
		t.Run(route, func(t *testing.T) {
			full := get(t, h, page.path)
			if full.Code != http.StatusOK {
				t.Fatalf("full page %s: %d", page.path, full.Code)
			}
			body := full.Body.String()
			for _, want := range []string{"<html", `class="[ topbar ]"`, `<main`, `id="app-shell"`, `id="stream-bar"`, `id="frame-state"`} {
				if !strings.Contains(body, want) {
					t.Errorf("full page lacks %q", want)
				}
			}
			if strings.Contains(body, "hx-swap-oob") || full.Header().Get("X-Harness-Fragment") != "" {
				t.Errorf("full page is marked as a fragment")
			}

			frag := fragment(h, page.path, page.from)
			if frag.Code != http.StatusOK {
				t.Fatalf("fragment %s from %s: %d", page.path, page.from, frag.Code)
			}
			fb := frag.Body.String()
			if !strings.Contains(fb, `<main`) {
				t.Errorf("fragment has no <main>:\n%.300s", fb)
			}
			for _, absent := range []string{"<html", `class="[ topbar ]"`, `id="rail"`, `id="app-shell"`} {
				if strings.Contains(fb, absent) {
					t.Errorf("fragment holds the frame: %q", absent)
				}
			}
			for _, oob := range []string{"<title>", `id="frame-state"`, `id="stream-bar"`} {
				if !strings.Contains(fb, oob) {
					t.Errorf("fragment lacks the out-of-band %s", oob)
				}
			}
			if frag.Header().Get("X-Harness-Fragment") != "page" || frag.Header().Get("HX-Retarget") != "#main" {
				t.Errorf("fragment is not marked: %v", frag.Header())
			}
			if m := regexp.MustCompile(`frame;dur=([0-9.]+)`).FindStringSubmatch(frag.Header().Get("Server-Timing")); m != nil && strings.Trim(m[1], "0.") != "" {
				t.Errorf("a fragment builds no frame, Server-Timing %q", frag.Header().Get("Server-Timing"))
			}
			// The same main: what the fragment's <main> shows is what the full
			// page's does.
			if got, want := mainOf(fb), mainOf(body); got != want {
				t.Errorf("the fragment's <main> differs from the full page's:\n%.200s\n%.200s", got, want)
			}
		})
	}
}

// A page outside any project answers a fragment too, and marks its top bar
// link open through the frame state.
func TestAPageOutsideAnyProjectNamesItsTopBarLinkInTheFrameState(t *testing.T) {
	h := newTestHandler(t, fragmentWorld())
	frag := fragment(h, "/projects", "/projects/new")
	if frag.Code != http.StatusOK {
		t.Fatalf("status %d", frag.Code)
	}
	if !strings.Contains(frag.Body.String(), `data-top="/projects"`) {
		t.Errorf("frame state does not name the projects link:\n%s", frag.Body)
	}
}

// A click to another frame (another project, in or out of a project, or with
// no current URL) is answered with the app shell of the new frame, without a
// new document (ADR 0064).
func TestAClickAcrossFramesAnswersTheAppShellOfTheNewFrame(t *testing.T) {
	h := newTestHandler(t, fragmentWorld())
	for name, c := range map[string]struct{ to, from, project string }{
		"to another project":         {"/projects/p1", "/projects/p2", "coding_pool"},
		"into a project":             {"/projects/p1/reviews", "/projects", "coding_pool"},
		"out of a project":           {"/projects", "/projects/p1", ""},
		"from the agents pages":      {"/projects", "/agents", ""},
		"with no current URL at all": {"/projects/p1", "", "coding_pool"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := fragment(h, c.to, c.from)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			body := rec.Body.String()
			if rec.Header().Get("X-Harness-Fragment") != "shell" || rec.Header().Get("HX-Retarget") != "" {
				t.Errorf("not marked as an app shell: %v", rec.Header())
			}
			for _, want := range []string{`<title>`, `id="app-shell"`, `class="[ topbar ]"`, `id="rail"`, `<main`, `id="stream-bar"`} {
				if !strings.Contains(body, want) {
					t.Errorf("app shell lacks %q", want)
				}
			}
			for _, absent := range []string{"<html", "<body", "<head>", "hx-swap-oob"} {
				if strings.Contains(body, absent) {
					t.Errorf("app shell holds %q: it is no document and no fragment", absent)
				}
			}
			if c.project != "" && !strings.Contains(body, `<option value="p1" selected>`+c.project) {
				t.Errorf("the picker does not show the new project %q", c.project)
			}
		})
	}
}

// Every page route registered in web.go is in the table above or named as one
// the test world cannot serve, so a new page cannot skip its fragment.
func TestEveryRegisteredPageRouteIsInTheFragmentTable(t *testing.T) {
	src, err := os.ReadFile("web.go")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for route := range fragmentPages {
		known[route] = true
	}
	for _, route := range notInTheTable {
		known[route] = true
	}
	// "GET /route" or "GET "+pkg.Href, then whatever tail, then s.page(.
	for _, m := range regexp.MustCompile(`mux\.Handle\("GET (?:([^"]+)"|"\+(\w+\.Href))[^,]*, s\.page\(`).FindAllStringSubmatch(string(src), -1) {
		route := m[1] + m[2]
		if !known[route] {
			t.Errorf("page route %q is in no fragment table: add it to fragmentPages or notInTheTable", route)
		}
	}
}

// fragment is a boosted click from the page at from, as htmx sends it.
func fragment(h http.Handler, path, from string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")
	req.Header.Set("HX-Target", "app-shell")
	if from != "" {
		req.Header.Set("HX-Current-URL", "http://127.0.0.1:8080"+from)
	}
	h.ServeHTTP(rec, req)
	return rec
}

// mainOf is the first <main …>…</main> in body.
func mainOf(body string) string {
	i := strings.Index(body, "<main")
	j := strings.Index(body, "</main>")
	if i < 0 || j < i {
		return ""
	}
	return body[i : j+len("</main>")]
}
