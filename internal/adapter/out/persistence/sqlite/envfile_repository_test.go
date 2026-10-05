package sqlite

import (
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func TestEnvFileRepository(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewEnvFileRepository(db)

	if _, err := r.Put(&domain.EnvFile{RepositoryID: "r1", Path: ".env", Content: "A=1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Put(&domain.EnvFile{RepositoryID: "r1", Path: "apps/api/.env", Content: "B=2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Put(&domain.EnvFile{RepositoryID: "r2", Path: ".env", Content: "OTHER=1"}); err != nil {
		t.Fatal(err)
	}
	updated, err := r.Put(&domain.EnvFile{RepositoryID: "r1", Path: ".env", Content: "A=2"})
	if err != nil || updated.Content != "A=2" || updated.UpdatedAt.IsZero() {
		t.Fatalf("replace: %+v %v", updated, err)
	}

	list := r.List("r1")
	if len(list) != 2 || list[0].Path != ".env" || list[0].Content != "A=2" || list[1].Path != "apps/api/.env" {
		t.Fatalf("list = %+v", list)
	}

	if err := r.Delete("r1", "apps/api/.env"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("r1", "apps/api/.env"); errs.Code(err) != "ENV_FILE_NOT_FOUND" {
		t.Fatalf("second delete: %v", err)
	}
	if err := r.DeleteByRepository("r1"); err != nil || len(r.List("r1")) != 0 || len(r.List("r2")) != 1 {
		t.Fatalf("delete by repository: %v, r1 %d, r2 %d", err, len(r.List("r1")), len(r.List("r2")))
	}
}
