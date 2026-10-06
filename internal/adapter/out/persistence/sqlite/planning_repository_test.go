package sqlite

import (
	"slices"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func TestTicketRepo_CRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTicketRepository(db)

	tk, err := r.Create("p1", "Fix login", "users cannot log in", domain.TicketStatusBacklog)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID == "" || tk.Status != domain.TicketStatusBacklog || tk.CreatedAt.IsZero() {
		t.Fatalf("bad create: %+v", tk)
	}
	if got := r.Get(tk.ID); got == nil || got.Title != "Fix login" {
		t.Fatalf("Get failed: %+v", got)
	}

	upd, err := r.Update(tk.ID, "Fix login now", "urgent", domain.TicketStatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Title != "Fix login now" || upd.Status != domain.TicketStatusInProgress {
		t.Fatalf("bad update: %+v", upd)
	}

	if list := r.ListByProject("p1"); len(list) != 1 {
		t.Fatalf("ListByProject want 1 got %d", len(list))
	}
	if list := r.ListByProject("other"); len(list) != 0 {
		t.Fatalf("ListByProject other want 0 got %d", len(list))
	}

	if err := r.Delete(tk.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(tk.ID); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if err := r.Delete("missing"); err == nil {
		t.Fatal("expected error deleting missing ticket")
	}
}

func TestDocumentRepo_CRUDAndLinks(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	tickets := NewTicketRepository(db)
	docs := NewDocumentRepository(db)

	tk, err := tickets.Create("p1", "Ticket", "", domain.TicketStatusBacklog)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := docs.Create("p1", "Spec", "# spec", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if d1.ID == "" || d1.CreatedAt.IsZero() {
		t.Fatalf("bad create: %+v", d1)
	}

	// A document exists standalone (no ticket) until linked.
	if got := docs.ListByTicket(tk.ID); len(got) != 0 {
		t.Fatalf("expected 0 linked docs, got %d", len(got))
	}

	// Link is idempotent.
	if err := docs.Link(tk.ID, d1.ID); err != nil {
		t.Fatal(err)
	}
	if err := docs.Link(tk.ID, d1.ID); err != nil {
		t.Fatalf("second Link should be a no-op, got %v", err)
	}
	if got := docs.ListByTicket(tk.ID); len(got) != 1 || got[0].ID != d1.ID {
		t.Fatalf("ListByTicket after link bad: %+v", got)
	}

	// Deleting the ticket drops the join row but keeps the document.
	if err := tickets.Delete(tk.ID); err != nil {
		t.Fatal(err)
	}
	if got := docs.Get(d1.ID); got == nil {
		t.Fatal("document should survive ticket deletion")
	}

	// Re-link, then Unlink.
	tk2, _ := tickets.Create("p1", "T2", "", domain.TicketStatusBacklog)
	if err := docs.Link(tk2.ID, d1.ID); err != nil {
		t.Fatal(err)
	}
	if err := docs.Unlink(tk2.ID, d1.ID); err != nil {
		t.Fatal(err)
	}
	if got := docs.ListByTicket(tk2.ID); len(got) != 0 {
		t.Fatalf("expected 0 after unlink, got %d", len(got))
	}

	// Deleting the document keeps tickets intact.
	if err := docs.Link(tk2.ID, d1.ID); err != nil {
		t.Fatal(err)
	}
	if err := docs.Delete(d1.ID); err != nil {
		t.Fatal(err)
	}
	if got := tickets.Get(tk2.ID); got == nil {
		t.Fatal("ticket should survive document deletion")
	}
	if got := docs.ListByTicket(tk2.ID); len(got) != 0 {
		t.Fatalf("join rows should be gone after doc delete, got %d", len(got))
	}
	if err := docs.Delete("missing"); err == nil {
		t.Fatal("expected error deleting missing document")
	}
}

func TestDocumentRepo_FormatPersists(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	docs := NewDocumentRepository(db)

	md, err := docs.Create("p1", "Notes", "# notes", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if md.Format != domain.DocumentFormatMarkdown {
		t.Fatalf("markdown create read back %q", md.Format)
	}
	page, err := docs.Create("p1", "Plan", "<!doctype html><html><body>plan</body></html>", domain.DocumentFormatHTML, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if got := docs.Get(page.ID); got.Format != domain.DocumentFormatHTML {
		t.Fatalf("html create read back %q", got.Format)
	}

	// An empty format on Update keeps the stored one; a format replaces it.
	kept, err := docs.Update(page.ID, "Plan v2", "<!doctype html><html><body>v2</body></html>", "")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Format != domain.DocumentFormatHTML || kept.Title != "Plan v2" {
		t.Fatalf("update with no format changed it: %+v", kept)
	}
	flipped, err := docs.Update(md.ID, "Notes", "<!doctype html><html><body>notes</body></html>", domain.DocumentFormatHTML)
	if err != nil {
		t.Fatal(err)
	}
	if flipped.Format != domain.DocumentFormatHTML {
		t.Fatalf("update with a format did not apply it: %+v", flipped)
	}
	for _, d := range docs.ListByProject("p1", "") {
		if d.Format == "" {
			t.Fatalf("listing lost the format: %+v", d)
		}
	}
}

// Rows written before the format column existed (or with an empty value)
// are Markdown: that is what every document was until now.
func TestDocumentRepo_LegacyRowsReadAsMarkdown(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	docs := NewDocumentRepository(db)
	d, err := docs.Create("p1", "Old", "# old", "", domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != domain.DocumentFormatMarkdown {
		t.Fatalf("empty format on create read back %q, want markdown", d.Format)
	}
	if err := db.Exec("UPDATE documents SET format = '' WHERE id = ?", d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got := docs.Get(d.ID); got.Format != domain.DocumentFormatMarkdown {
		t.Fatalf("legacy empty column read back %q, want markdown", got.Format)
	}
}

func TestDocumentRepo_ScopePersistsFiltersAndMoves(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	tickets := NewTicketRepository(db)
	docs := NewDocumentRepository(db)

	plan, err := docs.Create("p1", "Plan", "<!doctype html><html><body>plan</body></html>", domain.DocumentFormatHTML, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	arch, err := docs.Create("p1", "Architecture", "# arch", domain.DocumentFormatMarkdown, domain.DocumentScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Scope != domain.DocumentScopeTask || arch.Scope != domain.DocumentScopeProject {
		t.Fatalf("scopes read back %q, %q", plan.Scope, arch.Scope)
	}
	if _, err := docs.Create("other", "Elsewhere", "", domain.DocumentFormatMarkdown, domain.DocumentScopeProject); err != nil {
		t.Fatal(err)
	}
	ids := func(list []*domain.Document) []string {
		out := []string{}
		for _, d := range list {
			out = append(out, d.ID)
		}
		slices.Sort(out)
		return out
	}
	if got := ids(docs.ListByProject("p1", "")); len(got) != 2 {
		t.Fatalf("every scope: %v", got)
	}
	if got := ids(docs.ListByProject("p1", domain.DocumentScopeProject)); !slices.Equal(got, []string{arch.ID}) {
		t.Fatalf("project scope: %v", got)
	}
	if got := ids(docs.ListByProject("p1", domain.DocumentScopeTask)); !slices.Equal(got, []string{plan.ID}) {
		t.Fatalf("task scope: %v", got)
	}

	// A move keeps the ticket links and is a new version; the links are listable.
	t1, _ := tickets.Create("p1", "T1", "", domain.TicketStatusBacklog)
	t2, _ := tickets.Create("p1", "T2", "", domain.TicketStatusBacklog)
	for _, tk := range []*domain.Ticket{t1, t2} {
		if err := docs.Link(tk.ID, plan.ID); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(2 * time.Millisecond) // updated_at has millisecond precision
	moved, err := docs.SetScope(plan.ID, domain.DocumentScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Scope != domain.DocumentScopeProject || !moved.UpdatedAt.After(plan.UpdatedAt) {
		t.Fatalf("move: %+v (was %v)", moved, plan.UpdatedAt)
	}
	if got := docs.ListByTicket(t1.ID); len(got) != 1 || got[0].Scope != domain.DocumentScopeProject {
		t.Fatalf("the task lost the moved document: %+v", got)
	}
	linked := docs.ListTicketIDsByDocument(plan.ID)
	slices.Sort(linked)
	want := []string{t1.ID, t2.ID}
	slices.Sort(want)
	if !slices.Equal(linked, want) {
		t.Fatalf("ticket ids of the document: %v, want %v", linked, want)
	}
	if got := docs.ListTicketIDsByDocument(arch.ID); len(got) != 0 {
		t.Fatalf("an unlinked document has tickets: %v", got)
	}
	if _, err := docs.SetScope("missing", domain.DocumentScopeTask); err == nil {
		t.Fatal("expected DOCUMENT_NOT_FOUND")
	}
}

// Rows written before the scope column existed (or with an empty value) are
// task documents: that is what every document was until now.
func TestDocumentRepo_LegacyRowsReadAsTask(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	docs := NewDocumentRepository(db)
	d, err := docs.Create("p1", "Old", "# old", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE documents SET scope = '' WHERE id = ?", d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got := docs.Get(d.ID); got.Scope != domain.DocumentScopeTask {
		t.Fatalf("legacy empty column read back %q, want task", got.Scope)
	}
	if got := docs.ListByProject("p1", domain.DocumentScopeTask); len(got) != 1 {
		t.Fatalf("a legacy row is not listed under task scope: %+v", got)
	}
	if got := docs.ListByProject("p1", domain.DocumentScopeProject); len(got) != 0 {
		t.Fatalf("a legacy row is listed as a project document: %+v", got)
	}
}
