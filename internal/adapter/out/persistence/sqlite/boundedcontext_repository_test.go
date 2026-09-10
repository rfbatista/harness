package sqlite

import (
	"errors"
	"testing"

	"operators-mcp/internal/domain"
)

func TestBoundedContextRepo_CRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewBoundedContextRepository(db)

	terms := []domain.LanguageTerm{{Term: "Order", Definition: "A customer's purchase request"}}
	bc, err := r.Create("p1", "Billing", "Handles invoicing", terms)
	if err != nil {
		t.Fatal(err)
	}
	if bc.ID == "" || bc.ProjectID != "p1" || bc.Name != "Billing" || bc.Purpose != "Handles invoicing" {
		t.Fatalf("bad create: %+v", bc)
	}
	if len(bc.UbiquitousLanguage) != 1 || bc.UbiquitousLanguage[0].Term != "Order" {
		t.Fatalf("language not stored: %+v", bc.UbiquitousLanguage)
	}

	// The language terms must round-trip through the JSON column.
	got := r.Get(bc.ID)
	if got == nil || got.Name != "Billing" {
		t.Fatalf("Get failed: %+v", got)
	}
	if len(got.UbiquitousLanguage) != 1 || got.UbiquitousLanguage[0].Definition != "A customer's purchase request" {
		t.Fatalf("language did not round-trip: %+v", got.UbiquitousLanguage)
	}
	if got := r.Get("missing"); got != nil {
		t.Fatalf("Get missing should be nil, got %+v", got)
	}

	if _, err := r.Create("p1", "Shipping", "", nil); err != nil {
		t.Fatal(err)
	}
	if list := r.ListByProject("p1"); len(list) != 2 {
		t.Fatalf("ListByProject want 2 got %d", len(list))
	}
	if list := r.ListByProject("other"); len(list) != 0 {
		t.Fatalf("ListByProject other want 0 got %d", len(list))
	}

	updated, err := r.Update(bc.ID, "Invoicing", "New purpose", []domain.LanguageTerm{
		{Term: "Invoice", Definition: "A bill sent to the customer"},
		{Term: "Ledger", Definition: "Record of transactions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Invoicing" || updated.Purpose != "New purpose" || len(updated.UbiquitousLanguage) != 2 {
		t.Fatalf("bad update: %+v", updated)
	}

	if _, err := r.Update("missing", "x", "", nil); !isCode(err, "BOUNDED_CONTEXT_NOT_FOUND") {
		t.Fatalf("expected BOUNDED_CONTEXT_NOT_FOUND, got %v", err)
	}

	if err := r.Delete(bc.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(bc.ID); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if err := r.Delete("missing"); !isCode(err, "BOUNDED_CONTEXT_NOT_FOUND") {
		t.Fatalf("expected BOUNDED_CONTEXT_NOT_FOUND, got %v", err)
	}
}

func TestBoundedContextRepo_CreateRequiresName(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewBoundedContextRepository(db)
	if _, err := r.Create("p1", "", "purpose", nil); !isCode(err, "INVALID_NAME") {
		t.Fatalf("expected INVALID_NAME, got %v", err)
	}
}

func TestBoundedContextRepo_DeleteByProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewBoundedContextRepository(db)
	if _, err := r.Create("p1", "A", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create("p1", "B", "", nil); err != nil {
		t.Fatal(err)
	}
	keep, err := r.Create("p2", "C", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteByProject("p1"); err != nil {
		t.Fatal(err)
	}
	if list := r.ListByProject("p1"); len(list) != 0 {
		t.Fatalf("want 0 after DeleteByProject, got %d", len(list))
	}
	if got := r.Get(keep.ID); got == nil {
		t.Fatal("other project's context should survive")
	}
}

func TestZoneRepo_SetAndClearBoundedContext(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	zones := NewZoneRepository(db)

	z1, err := zones.Create("p1", "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	z2, err := zones.Create("p1", "web", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	linked, err := zones.SetBoundedContext(z1.ID, "bc1")
	if err != nil {
		t.Fatal(err)
	}
	if linked.BoundedContextID != "bc1" {
		t.Fatalf("link not set: %+v", linked)
	}
	if got := zones.Get(z1.ID); got.BoundedContextID != "bc1" {
		t.Fatalf("link not persisted: %+v", got)
	}

	// A regular Update must not clobber the link.
	if _, err := zones.Update(z1.ID, "api", "src/api/.*", "the API", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := zones.Get(z1.ID); got.BoundedContextID != "bc1" {
		t.Fatalf("Update clobbered the bounded context link: %+v", got)
	}

	// Empty id clears the link.
	cleared, err := zones.SetBoundedContext(z1.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.BoundedContextID != "" {
		t.Fatalf("link not cleared: %+v", cleared)
	}

	if _, err := zones.SetBoundedContext("missing", "bc1"); !isCode(err, "ZONE_NOT_FOUND") {
		t.Fatalf("expected ZONE_NOT_FOUND, got %v", err)
	}

	// Bulk clear unlinks only the matching zones.
	if _, err := zones.SetBoundedContext(z1.ID, "bc1"); err != nil {
		t.Fatal(err)
	}
	if _, err := zones.SetBoundedContext(z2.ID, "bc2"); err != nil {
		t.Fatal(err)
	}
	if err := zones.ClearBoundedContext("bc1"); err != nil {
		t.Fatal(err)
	}
	if got := zones.Get(z1.ID); got.BoundedContextID != "" {
		t.Fatalf("bc1 zone should be unlinked: %+v", got)
	}
	if got := zones.Get(z2.ID); got.BoundedContextID != "bc2" {
		t.Fatalf("bc2 zone should keep its link: %+v", got)
	}
}

// isCode reports whether err is a StructuredError with the given code.
func isCode(err error, code string) bool {
	var se *domain.StructuredError
	return errors.As(err, &se) && se.Code == code
}
