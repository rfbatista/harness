package projects

import (
	"context"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

func newRepoTestService(t *testing.T) (*Service, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	repos := sqlite.NewRepositoryRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(projects, repos, nil)
	return svc, p.ID
}

func TestCreateRepository_RequiresProject(t *testing.T) {
	svc, _ := newRepoTestService(t)
	_, err := svc.CreateRepository(context.Background(), "missing", "api", "", "https://github.com/o/r", "/tmp/r")
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	se, ok := err.(*domain.StructuredError)
	if !ok || se.Code != "PROJECT_NOT_FOUND" {
		t.Fatalf("want PROJECT_NOT_FOUND, got %v", err)
	}
}

func TestCreateAndListRepositories(t *testing.T) {
	svc, pid := newRepoTestService(t)
	created, err := svc.CreateRepository(context.Background(), pid, "api", "desc", "https://github.com/o/api", "/tmp/api")
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListRepositories(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("want 1 repo %q, got %d", created.ID, len(list))
	}
}
