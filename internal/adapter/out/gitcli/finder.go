package gitcli

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.RepositoryFinder = (*RepositoryFinder)(nil)

// RepositoryFinder walks a directory for git checkouts and reads their
// origin remote with git.
type RepositoryFinder struct{}

// NewRepositoryFinder returns the finder.
func NewRepositoryFinder() *RepositoryFinder { return &RepositoryFinder{} }

// skipDirs are never searched: they are not where checkouts live, and some
// are huge.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true}

// FindRepositories returns the checkouts at or under root, depth levels down
// at most, sorted by path. A directory holding .git (a directory, or a file
// for worktrees and submodules) is a checkout; its insides are not searched.
// Hidden directories are skipped. Unreadable directories are skipped too:
// one permission error must not hide every other checkout.
func (f *RepositoryFinder) FindRepositories(root string, depth int) ([]ports.FoundRepository, error) {
	root = filepath.Clean(root)
	var found []ports.FoundRepository
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		level := 0
		if rel != "." {
			level = strings.Count(rel, string(filepath.Separator)) + 1
			name := d.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return fs.SkipDir
			}
		}
		if isCheckout(path) {
			found = append(found, ports.FoundRepository{Path: path, Name: filepath.Base(path), Remote: originURL(path)})
			return fs.SkipDir
		}
		if level >= depth {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b ports.FoundRepository) int { return strings.Compare(a.Path, b.Path) })
	return found, nil
}

func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// originURL is the checkout's origin remote, or "" when it has none.
func originURL(dir string) string {
	out, err := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(out))
}
