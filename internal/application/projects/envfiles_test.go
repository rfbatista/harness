package projects

import (
	"context"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
)

func TestEnvFilesAreKeptPerRepositoryAndGoWithIt(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewEnvFileRepository(db)
	s := NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
	s.UseEnvFiles(store, gitcli.NewEnvFiles())
	ctx := context.Background()

	p, _ := s.CreateProject(ctx, "p", t.TempDir())
	r, _ := s.CreateRepository(ctx, p.ID, "api", "", "file:///x", t.TempDir())

	if _, err := s.SaveEnvFile(ctx, r.ID, ".env", strings.Repeat("x", 300<<10)); errs.Code(err) != "ENV_FILE_TOO_LARGE" {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := s.SaveEnvFile(ctx, r.ID, "/etc/hosts", "x"); errs.Code(err) != "INVALID_PATH" {
		t.Fatalf("absolute path: %v", err)
	}
	if _, err := s.ImportEnvFile(ctx, r.ID, ".env"); errs.Code(err) != "ENV_FILE_NOT_FOUND" {
		t.Fatalf("import of a missing file: %v", err)
	}
	if _, err := s.SaveEnvFile(ctx, r.ID, ".env", "A=1"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteRepository(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if left := store.List(r.ID); len(left) != 0 {
		t.Fatalf("env files outlived their repository: %+v", left)
	}

	r2, _ := s.CreateRepository(ctx, p.ID, "web", "", "file:///y", t.TempDir())
	_, _ = s.SaveEnvFile(ctx, r2.ID, ".env", "B=2")
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if left := store.List(r2.ID); len(left) != 0 {
		t.Fatalf("env files outlived their project: %+v", left)
	}
}
