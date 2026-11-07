package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
)

func TestARepositoryRootDirIsStoredAbsoluteAndCleaned(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cases := []struct{ name, in, want string }{
		{"tilde path", "~/x", filepath.Join(home, "x")},
		{"tilde alone", "~", home},
		{"absolute is cleaned", "/does/not/exist/../yet/", "/does/not/yet"},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, pid := newRepoTestService(t)
			created, err := svc.CreateRepository(context.Background(), pid, "api", "", "https://github.com/o/api", c.in)
			if err != nil {
				t.Fatal(err)
			}
			if created.RootDir != c.want {
				t.Fatalf("create stored %q, want %q", created.RootDir, c.want)
			}
			other, err := svc.CreateRepository(context.Background(), pid, "web", "", "https://github.com/o/web", "/tmp/web")
			if err != nil {
				t.Fatal(err)
			}
			updated, err := svc.UpdateRepository(context.Background(), other.ID, "web", "", "https://github.com/o/web", c.in)
			if err != nil {
				t.Fatal(err)
			}
			wantUpdated := c.want
			if c.in == "" {
				wantUpdated = "/tmp/web" // an empty value on update keeps the stored one
			}
			if updated.RootDir != wantUpdated {
				t.Fatalf("update stored %q, want %q", updated.RootDir, wantUpdated)
			}
		})
	}
}

func TestARelativeRepositoryRootDirIsRefusedAndNothingChanges(t *testing.T) {
	svc, pid := newRepoTestService(t)
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, pid, "api", "", "https://github.com/o/api", "code/app"); !isInvalidRoot(err) {
		t.Fatalf("create: want INVALID_ROOT, got %v", err)
	}
	repo, err := svc.CreateRepository(ctx, pid, "web", "", "https://github.com/o/web", "/tmp/web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateRepository(ctx, repo.ID, "renamed", "", "https://github.com/o/web", "code/app"); !isInvalidRoot(err) {
		t.Fatalf("update: want INVALID_ROOT, got %v", err)
	}
	list, err := svc.ListRepositories(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "web" || list[0].RootDir != "/tmp/web" {
		t.Fatalf("a refused save changed the stored repositories: %+v", list)
	}
}

func isInvalidRoot(err error) bool {
	se, ok := err.(*domain.StructuredError)
	return ok && se.Code == "INVALID_ROOT"
}
