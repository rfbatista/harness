package filesystem

import (
	"os"
	"path/filepath"
	"regexp"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure matcher implements ports.PathMatcher at compile time.
var _ ports.PathMatcher = (*Matcher)(nil)

// Matcher implements PathMatcher using the OS filesystem.
type Matcher struct{}

// NewMatcher returns a new filesystem path matcher.
func NewMatcher() *Matcher {
	return &Matcher{}
}

// ListMatchingPaths walks root (or cwd if empty), collects relative paths (dirs and files),
// and returns those matching the regex pattern. Invalid pattern returns StructuredError.
func (m *Matcher) ListMatchingPaths(root, pattern string) ([]string, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
	}
	if !info.IsDir() {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: "root is not a directory"}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, &domain.StructuredError{Code: "INVALID_PATTERN", Message: err.Error()}
	}
	var paths []string
	err = filepath.Walk(root, func(p string, info os.FileInfo, errWalk error) error {
		if errWalk != nil {
			return errWalk
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if re.MatchString(rel) {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
	}
	return paths, nil
}
