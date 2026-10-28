package sqlite

import (
	"context"
	"testing"
	"time"

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

	if got := r.Get(context.Background(), ws.ID); got == nil || got.Path != "/tmp/wt/feature-x" {
		t.Fatalf("Get failed: %+v", got)
	}
	if got := r.Get(context.Background(), "missing"); got != nil {
		t.Fatalf("Get missing should be nil, got %+v", got)
	}

	// Second workspace under the same repository proves one-to-many.
	if _, err := r.Create(domain.Workspace{RepositoryID: "r1", Name: "feature-y", Branch: "feature-y", Path: "/tmp/wt/feature-y"}); err != nil {
		t.Fatal(err)
	}
	if list := r.ListByRepository("r1"); len(list) != 2 {
		t.Fatalf("ListByRepository want 2 got %d", len(list))
	}
	if list := r.ListByIDs(context.Background(), []string{ws.ID, "missing"}); len(list) != 1 || list[0].ID != ws.ID {
		t.Fatalf("ListByIDs want only %s, got %+v", ws.ID, list)
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
	if got := r.Get(context.Background(), ws.ID); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if err := r.Delete("missing"); err == nil {
		t.Fatal("expected error deleting missing workspace")
	}
}

func TestWorkspaceRepository_KeepsWhenItsBranchWasLastPushed(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewWorkspaceRepository(db)
	ws, err := repo.Create(domain.Workspace{RepositoryID: "r1", Name: "a", Branch: "a", Path: "/wt/a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.Get(context.Background(), ws.ID); got.PushedAt != nil {
		t.Fatalf("a new workspace was pushed at %v", got.PushedAt)
	}
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if err := repo.SetPushedAt(ws.ID, at); err != nil {
		t.Fatal(err)
	}
	// Another repository over the same database, as after a restart.
	if got := NewWorkspaceRepository(db).Get(context.Background(), ws.ID); got.PushedAt == nil || !got.PushedAt.Equal(at) {
		t.Errorf("pushed_at = %v, want %v", got.PushedAt, at)
	}
	if err := repo.SetPushedAt("nope", at); err == nil {
		t.Error("an unknown workspace was recorded")
	}
}
