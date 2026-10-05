package sqlite

import (
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func TestRunCommandRepository(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunCommandRepository(db)
	for _, c := range []*domain.RunCommand{
		{RepositoryID: "r1", Name: "web", Command: "npm run dev"},
		{RepositoryID: "r1", Name: "server", Command: "make air"},
		{RepositoryID: "r2", Name: "server", Command: "go run ."},
	} {
		if _, err := r.Put(c); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := r.Put(&domain.RunCommand{RepositoryID: "r1", Name: "server", Command: "make server"}); err != nil || c.Command != "make server" {
		t.Fatalf("replace: %+v %v", c, err)
	}
	list := r.List("r1")
	if len(list) != 2 || list[0].Name != "server" || list[0].Command != "make server" || list[1].Name != "web" {
		t.Fatalf("list = %+v", list)
	}
	if err := r.Delete("r1", "web"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("r1", "web"); errs.Code(err) != "RUN_COMMAND_NOT_FOUND" {
		t.Fatalf("second delete: %v", err)
	}
	if err := r.DeleteByRepository("r1"); err != nil || len(r.List("r1")) != 0 || len(r.List("r2")) != 1 {
		t.Fatalf("delete by repository: %v", err)
	}
}
