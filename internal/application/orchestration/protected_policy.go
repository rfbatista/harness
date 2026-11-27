package orchestration

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"operators-mcp/internal/domain"
)

// ProtectedSandboxPaths are the paths of a worktree a gated session's sandbox
// makes read-only (ADR 0012): the protected globs of its kept configuration
// turned into the paths a sandbox policy can name, minus what its scope
// unlocks. A policy names paths and their subtrees, not globs, so:
//
//   - a name with no wildcard ("check.sh") is its path, even when the file does
//     not exist yet, so the driver that can (Seatbelt) also refuses creating it;
//   - "dir/**" with a plain directory is that directory's subtree, new files
//     included, unless the scope unlocks something inside it, when the files
//     there now are listed one by one, minus the unlocked ones;
//   - any other wildcard glob is the paths that match it now.
//
// A file a wildcard glob would match later is not covered by the OS: the
// protected-path hook still refuses it, and bubblewrap, which can bind only
// what exists, leaves a protected name that does not exist yet to the hook.
func ProtectedSandboxPaths(worktree string, globs, scope []string) []string {
	if worktree == "" {
		return nil
	}
	unlocked := func(rel string) bool { _, ok := domain.ProtectedGlob(scope, rel); return ok }
	var out []string
	add := func(rel string) {
		p := filepath.Join(worktree, filepath.FromSlash(rel))
		if rel != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	walk := func(glob string, root string) {
		_ = filepath.WalkDir(filepath.Join(worktree, filepath.FromSlash(root)), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, rerr := filepath.Rel(worktree, p)
			if rerr != nil || rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if rel == root { // the folder being listed is not one of its files
				return nil
			}
			if d.IsDir() && d.Name() == ".git" {
				return fs.SkipDir
			}
			if domain.MatchProtectedPath(glob, rel) && !unlocked(rel) {
				add(rel)
				if d.IsDir() {
					return fs.SkipDir
				}
			}
			return nil
		})
	}
	// subtree protects base whole, or, when the scope unlocks something inside
	// it, the files there now but the unlocked ones.
	subtree := func(base string) {
		switch {
		case unlocked(base):
		case scopeReaches(scope, base):
			walk(base+"/**", base)
		default:
			add(base)
		}
	}
	for _, g := range globs {
		g = strings.Trim(strings.TrimSpace(g), "/")
		switch {
		case g == "":
		case !strings.ContainsAny(g, "*?["):
			subtree(g)
		case strings.HasSuffix(g, "/**") && !strings.ContainsAny(strings.TrimSuffix(g, "/**"), "*?["):
			subtree(strings.TrimSuffix(g, "/**"))
		default:
			walk(g, ".")
		}
	}
	slices.Sort(out)
	return out
}

// scopeReaches says whether a scope glob unlocks something inside dir or below
// the plain path rel: its leading plain segments run into rel.
func scopeReaches(scope []string, rel string) bool {
	for _, s := range scope {
		s = strings.Trim(s, "/")
		if s == rel || strings.HasPrefix(s, rel+"/") {
			return true
		}
	}
	return false
}
