// Package artifacts records what a session published for the user to look
// at, serves the bytes from the session worktree, and announces each publish
// on the session's event stream. Nothing is ever scanned: an artifact exists
// only because a session published it.
package artifacts

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

func outside(msg string) error {
	return &domain.StructuredError{Code: "ARTIFACT_PATH_OUTSIDE_WORKTREE", Message: msg}
}

func notFound(msg string) error {
	return &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: msg}
}

// resolveInWorktree turns p — worktree-relative, or absolute — into the real
// path of an existing file inside root, plus its worktree-relative POSIX
// form. The lexical check comes first so an escape that also does not exist
// reads as an escape; symlinks on both sides are then resolved, so a link
// that leaves the worktree is caught and a worktree that itself lives under
// a symlinked directory (/var → /private/var on macOS) still matches.
// Anything under .git is treated as outside.
func resolveInWorktree(root, p string) (abs, rel string, err error) {
	rootReal, rerr := filepath.EvalSymlinks(root)
	if rerr != nil {
		return "", "", outside("the session worktree " + root + " is gone")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(rootReal, p)
	}
	p = filepath.Clean(p)
	if !within(root, p) && !within(rootReal, p) {
		return "", "", outside(p + " is outside the session worktree " + root)
	}
	// Lexically first, so a .git path that does not exist still reads as the
	// rule it breaks rather than as a missing file.
	lexRoot := root
	if within(rootReal, p) {
		lexRoot = rootReal
	}
	if lexRel, _ := filepath.Rel(lexRoot, p); underGit(lexRel) {
		return "", "", outside(".git contents are not publishable")
	}
	real, serr := filepath.EvalSymlinks(p)
	if serr != nil {
		if errors.Is(serr, fs.ErrNotExist) {
			return "", "", notFound(p + " does not exist in the worktree")
		}
		return "", "", notFound(p + " cannot be read: " + serr.Error())
	}
	if !within(rootReal, real) {
		return "", "", outside(p + " is a symlink to " + real + ", outside the session worktree")
	}
	r, _ := filepath.Rel(rootReal, real)
	if underGit(r) {
		return "", "", outside(".git contents are not publishable")
	}
	return real, filepath.ToSlash(r), nil
}

// underGit reports whether a worktree-relative path has a .git segment, in
// any case: on a case-insensitive filesystem .GIT is the same directory.
func underGit(rel string) bool {
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if strings.EqualFold(seg, ".git") {
			return true
		}
	}
	return false
}

// within reports whether p (clean, absolute) is root or lies under it.
func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
