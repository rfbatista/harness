// Package keychainshim makes the directory holding the `security` shim that
// keeps agents out of the person's keychain (ADR 0022): on macOS claude
// writes its login to the Keychain through `security`, and falls back to
// .credentials.json in the agent home only when that fails.
package keychainshim

import (
	"os"
	"path/filepath"

	"operators-mcp/internal/ports"
)

// Dir is the shim directory, one per server run.
type Dir struct{ path string }

// New makes the directory with `security` linked to shimBinary. Off macOS, or
// without a shim binary, it makes nothing and Path is empty.
func New(shimBinary, goos string) (*Dir, error) {
	if goos != "darwin" || shimBinary == "" {
		return &Dir{}, nil
	}
	dir, err := os.MkdirTemp("", "harness-keychain-shim-*")
	if err != nil {
		return nil, err
	}
	if err := os.Symlink(shimBinary, filepath.Join(dir, ports.SecurityShimCommand)); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &Dir{path: dir}, nil
}

// Path is the directory, "" when there is none.
func (d *Dir) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

// Close removes the directory.
func (d *Dir) Close() {
	if d != nil && d.path != "" {
		_ = os.RemoveAll(d.path)
	}
}
