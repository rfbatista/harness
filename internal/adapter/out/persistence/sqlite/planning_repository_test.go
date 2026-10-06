package sqlite

import (
	"testing"

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
	d1, err := docs.Create("p1", "Spec", "# spec", domain.DocumentFormatMarkdown)
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

	md, err := docs.Create("p1", "Notes", "# notes", domain.DocumentFormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if md.Format != domain.DocumentFormatMarkdown {
		t.Fatalf("markdown create read back %q", md.Format)
	}
	page, err := docs.Create("p1", "Plan", "<!doctype html><html><body>plan</body></html>", domain.DocumentFormatHTML)
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
	for _, d := range docs.ListByProject("p1") {
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
	d, err := docs.Create("p1", "Old", "# old", "")
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
