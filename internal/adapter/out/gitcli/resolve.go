package gitcli

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// shimGit is where macOS keeps its git shim: a stub that asks xcrun for the
// developer tools' git and runs it, which costs every git run an extra
// process start and a lookup.
const shimGit = "/usr/bin/git"

// ResolveGit is the git binary the server runs, resolved once at startup:
// setting when it is set; otherwise the first git on PATH, unless that is
// macOS's shim, in which case the git the shim would run (xcrun -f git), or
// still the shim when xcrun cannot say. With no git on PATH it is "git", so
// the failure surfaces where git is first run.
func ResolveGit(setting string) string {
	return resolveGit(setting, exec.LookPath, xcrunGit)
}

func resolveGit(setting string, lookPath func(string) (string, error), xcrun func() (string, error)) string {
	if setting != "" {
		return setting
	}
	path, err := lookPath("git")
	if err != nil {
		return "git"
	}
	if path != shimGit {
		return path
	}
	if real, err := xcrun(); err == nil && real != "" {
		return real
	}
	return path
}

// xcrunGit is the git the developer tools' shim runs, by xcrun -f git.
func xcrunGit() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("xcrun: not macOS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultReadDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "xcrun", "-f", "git")
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
