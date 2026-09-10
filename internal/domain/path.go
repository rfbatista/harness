package domain

import (
	"path/filepath"
	"strings"
)

// NormalizePath returns path with forward slashes, no leading slash.
// Domain rule for consistent path representation in zones.
func NormalizePath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/")
}
