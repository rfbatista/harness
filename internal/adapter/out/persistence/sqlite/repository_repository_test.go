package sqlite

import "testing"

func TestRepositoryRepo_CRUD(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRepositoryRepository(db)

	repo, err := r.Create("p1", "api", "backend service", "https://example.com/api.git", "/src/api")
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID == "" || repo.ProjectID != "p1" || repo.Name != "api" || repo.URL != "https://example.com/api.git" {
		t.Fatalf("bad create: %+v", repo)
	}
	if repo.IgnoredPaths == nil {
		t.Fatalf("IgnoredPaths should be non-nil, got %+v", repo)
	}

	if got := r.Get(repo.ID); got == nil || got.RootDir != "/src/api" {
		t.Fatalf("Get failed: %+v", got)
	}

	upd, err := r.Update(repo.ID, "api-v2", "renamed", "https://example.com/api2.git", "/src/api2")
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "api-v2" || upd.Description != "renamed" || upd.RootDir != "/src/api2" {
		t.Fatalf("bad update: %+v", upd)
	}

	// A second repository under the same project proves one-to-many.
	if _, err := r.Create("p1", "web", "", "https://example.com/web.git", "/src/web"); err != nil {
		t.Fatal(err)
	}
	if list := r.ListByProject("p1"); len(list) != 2 {
		t.Fatalf("ListByProject want 2 got %d", len(list))
	}
	if list := r.ListByProject("other"); len(list) != 0 {
		t.Fatalf("ListByProject other want 0 got %d", len(list))
	}

	if err := r.Delete(repo.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.Get(repo.ID); got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	if err := r.Delete("missing"); err == nil {
		t.Fatal("expected error deleting missing repository")
	}
}

func TestRepositoryRepo_IgnoredPaths(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRepositoryRepository(db)

	repo, err := r.Create("p1", "api", "", "https://example.com/api.git", "/src/api")
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.AddIgnoredPath(repo.ID, "node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IgnoredPaths) != 1 || got.IgnoredPaths[0] != "node_modules" {
		t.Fatalf("AddIgnoredPath bad: %+v", got.IgnoredPaths)
	}

	// Adding the same path again is a no-op.
	got, err = r.AddIgnoredPath(repo.ID, "node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IgnoredPaths) != 1 {
		t.Fatalf("AddIgnoredPath should be idempotent, got %+v", got.IgnoredPaths)
	}

	got, err = r.RemoveIgnoredPath(repo.ID, "node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IgnoredPaths) != 0 {
		t.Fatalf("RemoveIgnoredPath bad: %+v", got.IgnoredPaths)
	}

	if _, err := r.AddIgnoredPath("missing", "x"); err == nil {
		t.Fatal("expected error for missing repository")
	}
}
