package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeDocs is the documents linked to each task, by task id.
type fakeDocs map[string][]*domain.Document

func (f fakeDocs) ListTicketDocuments(ticketID string) []*domain.Document { return f[ticketID] }

func documentsBoard() world {
	w := board()
	w.docs = fakeDocs{"t-feed": {
		{ID: "d-plan", ProjectID: "p1", Title: "Plan", UpdatedAt: now.Add(-3 * time.Hour),
			Content: "# The plan\n\n1. Add the feed\n2. Test it\n\n| step | who |\n|---|---|\n| feed | lead |\n"},
		{ID: "d-notes", ProjectID: "p1", Title: "Handoff notes", UpdatedAt: now.Add(-5 * time.Minute),
			Content: "Read `feed.go` first.\n\n<script>alert(1)</script>\n\n[click](javascript:alert(2)) and [docs](https://example.com)\n"},
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
		"2 documents",
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
	if !strings.Contains(body, `data-count="2"`) {
		t.Error("the link does not carry the count")
	}
	// Without a document reader, no link.
	if body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String(); strings.Contains(body, "/documents") {
		t.Error("documents linked with no reader")
	}
}
