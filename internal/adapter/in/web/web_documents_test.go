package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/domain"
)

// fakeDocs is the documents linked to each task, by task id.
type fakeDocs map[string][]*domain.Document

func (f fakeDocs) ListTicketDocuments(ticketID string) []*domain.Document { return f[ticketID] }

func documentsBoard() world {
	w := board()
	w.docs = fakeDocs{"t-feed": {
		{ID: "d-plan", ProjectID: "p1", Title: "Plan", Format: domain.DocumentFormatMarkdown, UpdatedAt: now.Add(-3 * time.Hour),
			Content: "# The plan\n\n1. Add the feed\n2. Test it\n\n| step | who |\n|---|---|\n| feed | lead |\n"},
		// No Format: a row from before documents had one.
		{ID: "d-notes", ProjectID: "p1", Title: "Handoff notes", UpdatedAt: now.Add(-5 * time.Minute),
			Content: "Read `feed.go` first.\n\n<script>alert(1)</script>\n\n[click](javascript:alert(2)) and [docs](https://example.com)\n"},
		{ID: "d-page", ProjectID: "p1", Title: "Plan page", Format: domain.DocumentFormatHTML, UpdatedAt: now.Add(-6 * time.Hour),
			Content: "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Plan page</title></head><body><h1>The page</h1><script>try{parent.document.title='pwned'}catch(e){}parent.postMessage('ran','*')</script></body></html>"},
	}}
	return w
}

func TestDocumentsPageOpensTheNewestRenderedSafely(t *testing.T) {
	rec := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-feed/documents")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"3 documents",
		`href="/projects/p1/tasks/t-feed/documents/d-notes" aria-current="page"`, // newest first, open
		`href="/projects/p1/tasks/t-feed/documents/d-plan"`,
		"<code>feed.go</code>",           // rendered as Markdown
		`<a href="https://example.com">`, // a safe link stays
		"5m",
		`data-ticket-id="t-feed"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, unsafe := range []string{"<script>alert(1)", "javascript:alert"} {
		if strings.Contains(body, unsafe) {
			t.Errorf("an agent's document put %q in the page", unsafe)
		}
	}
	if strings.Index(body, "d-notes") > strings.Index(body, "d-plan") {
		t.Error("documents are not newest first")
	}
}

func TestDocumentsPageOpensTheNamedDocument(t *testing.T) {
	rec := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-feed/documents/d-plan")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "<h1>The plan</h1>") || !strings.Contains(body, "<table>") {
		t.Fatalf("status %d; the plan is not rendered (GFM table included):\n%s", rec.Code, body)
	}
	if !strings.Contains(body, "<title>Plan · Add SSE feed") {
		t.Error("the title names the open document")
	}
}

func TestDocumentsPageRefusesADocumentOfAnotherTask(t *testing.T) {
	w := documentsBoard()
	w.docs["t-docs"] = []*domain.Document{{ID: "d-other", ProjectID: "p1", Title: "Other"}}
	if rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed/documents/d-other"); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if rec := get(t, newTestHandler(t, w), "/projects/p2/tasks/t-feed/documents"); rec.Code != http.StatusNotFound {
		t.Fatalf("a task under another project: status %d, want 404", rec.Code)
	}
}

func TestDocumentsPageWithNoneSaysHowTheyArrive(t *testing.T) {
	rec := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-docs/documents")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No documents on this task yet") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body)
	}
}

func TestTaskToolbarLinksItsDocumentsWithACount(t *testing.T) {
	body := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-feed").Body.String()
	if !strings.Contains(body, `href="/projects/p1/tasks/t-feed/documents"`) || !strings.Contains(body, `x-data="tasksDocumentWatch"`) {
		t.Fatal("the task page does not link its documents")
	}
	if !strings.Contains(body, `data-count="3"`) {
		t.Error("the link does not carry the count")
	}
	// Without a document reader, no link.
	if body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String(); strings.Contains(body, "/documents") {
		t.Error("documents linked with no reader")
	}
}

func TestDocumentsPageFramesAnHTMLDocument(t *testing.T) {
	rec := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-feed/documents/d-page")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<iframe sandbox="allow-scripts" referrerpolicy="no-referrer"`,
		`src="/projects/p1/tasks/t-feed/documents/d-page/view?v=`,
		`title="Plan page"`,
		`aria-label="Plan page"`,
		"leaves the frame",
		`href="/projects/p1/tasks/t-feed/documents/d-page/view?v=`, // open in a new tab
		"<title>Plan page · Add SSE feed",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, inline := range []string{"<h1>The page</h1>", "pwned", `class="[ prose ]`} {
		if strings.Contains(body, inline) {
			t.Errorf("an HTML document's body reached the page inline: %q", inline)
		}
	}
}

func TestDocumentViewServesTheHTMLUnderTheArtifactCSP(t *testing.T) {
	w := documentsBoard()
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed/documents/d-page/view?v=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got, want := rec.Body.String(), w.docs["t-feed"][2].Content; got != want {
		t.Fatalf("body altered:\n%s", got)
	}
	for k, want := range map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": httpapi.ArtifactCSP,
		"X-Content-Type-Options":  "nosniff",
		"Content-Disposition":     "inline",
		"Cache-Control":           "no-store",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestDocumentViewIsScopedToTheTask(t *testing.T) {
	w := documentsBoard()
	w.docs["t-docs"] = []*domain.Document{{ID: "d-other", ProjectID: "p1", Title: "Other", Format: domain.DocumentFormatHTML, Content: "<!doctype html><html><body>x</body></html>"}}
	h := newTestHandler(t, w)
	for _, path := range []string{
		"/projects/p1/tasks/t-feed/documents/d-other/view", // linked to another task
		"/projects/p2/tasks/t-feed/documents/d-page/view",  // task under another project
		"/projects/p1/tasks/t-feed/documents/d-plan/view",  // a markdown document has no page
		"/projects/p1/tasks/t-feed/documents/missing/view",
	} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

// A document with no format (a row from before the change, or a fake) is Markdown.
func TestDocumentsPageTreatsAnEmptyFormatAsMarkdown(t *testing.T) {
	body := get(t, newTestHandler(t, documentsBoard()), "/projects/p1/tasks/t-feed/documents/d-notes").Body.String()
	if !strings.Contains(body, "<code>feed.go</code>") || strings.Contains(body, "<iframe") {
		t.Fatalf("legacy document not rendered as Markdown:\n%s", body)
	}
}

// The stylesheet an HTML document may link to match the UI's prose look.
func TestDocumentStylesheetIsServed(t *testing.T) {
	rec := get(t, newTestHandler(t, documentsBoard()), "/static/document.css")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
