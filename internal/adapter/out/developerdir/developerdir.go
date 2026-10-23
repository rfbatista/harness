// Package developerdir finds macOS's active developer directory.
//
// Apple's /usr/bin tools (git, clang, ...) are shims that ask xcrun for the
// real tool in the active developer directory and keep a user-wide cache of
// the answers in the system temp dir (xcrun_db), whatever TMPDIR says: xcrun
// has no per-session cache location. The sandbox keeps that cache read-only,
// since a session able to write it could poison what the person's own xcrun
// later runs. A session instead gets the developer directory's usr/bin on its
// PATH and runs the tools directly, never reaching the shim.
package developerdir

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const timeout = 3 * time.Second

// Bin is the active developer directory's usr/bin (xcode-select -p, then
// usr/bin). It fails off macOS, and where there is no developer directory.
func Bin() (string, error) {
	return bin(runtime.GOOS, func(ctx context.Context) ([]byte, error) {
		return exec.CommandContext(ctx, "xcode-select", "-p").Output()
	})
}

func bin(goos string, run func(context.Context) ([]byte, error)) (string, error) {
	if goos != "darwin" {
		return "", errors.New("developerdir: not macOS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := run(ctx)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("developerdir: xcode-select named no directory")
	}
	return filepath.Join(dir, "usr", "bin"), nil
}
