package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/ports"
)

type stubFinder struct {
	root  string
	depth int
}

func (f *stubFinder) FindRepositories(root string, depth int) ([]ports.FoundRepository, error) {
	f.root, f.depth = root, depth
	return []ports.FoundRepository{{Path: filepath.Join(root, "api"), Name: "api"}}, nil
}

func TestFindRepositoriesResolvesTheDirectoryFirst(t *testing.T) {
	s := NewService(nil, nil, nil)
	finder := &stubFinder{}
	s.UseFinder(finder)

	dir := t.TempDir()
	root, found, err := s.FindRepositories(context.Background(), dir+"/")
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Clean(dir) || finder.root != root || finder.depth != findDepth || len(found) != 1 {
		t.Fatalf("root %q, finder %+v, found %+v", root, finder, found)
	}

	home, _ := os.UserHomeDir()
	if root, _, err := s.FindRepositories(context.Background(), "~"); err != nil || root != home {
		t.Errorf("~ resolves to the home directory: %q %v", root, err)
	}

	for _, bad := range []string{"relative/dir", filepath.Join(dir, "missing"), ""} {
		if _, _, err := s.FindRepositories(context.Background(), bad); errs.Code(err) != "INVALID_ROOT" {
			t.Errorf("%q: got %v, want INVALID_ROOT", bad, err)
		}
	}
}

func TestFindRepositoriesWithoutAFinderIsInternal(t *testing.T) {
	if _, _, err := NewService(nil, nil, nil).FindRepositories(context.Background(), t.TempDir()); errs.Code(err) != "INTERNAL" {
		t.Fatalf("got %v", err)
	}
}
