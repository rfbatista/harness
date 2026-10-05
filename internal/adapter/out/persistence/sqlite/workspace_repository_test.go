package sqlite

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestWorkspaceRepo_CRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewWorkspaceRepository(db)

	ws, err := r.Create(domain.Workspace{RepositoryID: "r1", Name: "feature-x", Branch: "feature-x", Path: "/tmp/wt/feature-x"})
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID == "" || ws.RepositoryID != "r1" || ws.Name != "feature-x" || ws.Branch != "feature-x" || ws.Path != "/tmp/wt/feature-x" {
		t.Fatalf("bad create: %+v", ws)
	}
	if ws.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt should be set: %+v", ws)
	}

	if got := r.Get(ws.ID); got == nil || got.Path != "/tmp/wt/feature-x" {
		t.Fatalf("Get failed: %+v", got)
	}
	if got := r.Get("missing"); got != nil {
		t.Fatalf("Get missing should be nil, got %+v", got)
	}

	// Second workspace under the same repository proves one-to-many.
	if _, err := r.Create(domain.Workspace{RepositoryID: "r1", Name: "feature-y", Branch: "feature-y", Path: "/tmp/wt/feature-y"}); err != nil {
		t.Fatal(err)
	}
	if list := r.ListByRepository("r1"); len(list) != 2 {
		t.Fatalf("ListByRepository want 2 got %d", len(list))
	}
	if list := r.ListByRepository("other"); len(list) != 0 {
		t.Fatalf("ListByRepository other want 0 got %d", len(list))
	}

	// The unique index rejects a duplicate name within the same repository,
	// but allows the same name under another repository.
	if _, err := r.Create(domain.Workspace{RepositoryID: "r1", Name: "feature-x", Branch: "other-branch", Path: "/tmp/wt/dup"}); err == nil {
		t.Fatal("expected error for duplicate name in same repository")
	}
	if _, err := r.Create(domain.Workspace{RepositoryID: "r2", Name: "feature-x", Branch: "feature-x", Path: "/tmp/wt2/feature-x"}); err != nil {
		t.Fatalf("same name in another repository should work: %v", err)
	}

	if err := r.Delete(ws.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(ws.ID); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if err := r.Delete("missing"); err == nil {
		t.Fatal("expected error deleting missing workspace")
	}
}
