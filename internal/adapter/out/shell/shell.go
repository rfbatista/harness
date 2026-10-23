// Package shell holds the ports.Shell adapters that run commands on this
// machine: Direct, which executes the program itself, and Login, which runs
// it through the user's login shell so their profile sets PATH, version
// managers and API keys.
package shell

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Shell = Direct{}
	_ ports.Shell = Login{}

	_ ports.ConfiningShell = Login{}
)

// Direct executes the program with this process's environment plus the
// command's.
type Direct struct{}

// Prepare resolves the program on PATH unless it is a path already.
func (Direct) Prepare(_ context.Context, cmd ports.ShellCommand) (ports.ProcessSpec, error) {
	path := cmd.Program
	if !filepath.IsAbs(path) {
		found, err := exec.LookPath(path)
		if err != nil {
			return ports.ProcessSpec{}, &domain.StructuredError{
				Code:    "AGENT_CLI_NOT_FOUND",
				Message: cmd.Program + " not found on PATH",
			}
		}
		path = found
	}
	return ports.ProcessSpec{Path: path, Args: slices.Clone(cmd.Args), Dir: cmd.Dir, Env: env(cmd)}, nil
}

func (Direct) CheckDir(_ context.Context, dir string) error { return checkDir(dir) }

// Login runs the program through a login shell: `$SHELL -l -c 'exec "$0"
// "$@"' program args...`, so the program is found on the PATH the user's
// profile sets. A program that is not there fails inside the terminal, as it
// would when typed. A command's EnvFile is sourced after the profile, so the
// profile cannot override what it sets (the session's git credentials).
// The session's PATH shims (ports.ShimDirEnv) go back first on PATH after
// the profile, which may have reordered it (macOS's path_helper does), so
// writes into the virtual shell cannot bypass the edit proxy.
type Login struct {
	// Shell is the shell to run; empty means $SHELL, else /bin/sh.
	Shell string
}

func (l Login) Prepare(_ context.Context, cmd ports.ShellCommand) (ports.ProcessSpec, error) {
	sh := l.Shell
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		sh = "/bin/sh"
	}
	args := append([]string{"-l", "-c", shimsFirst + `exec "$0" "$@"`, cmd.Program}, cmd.Args...)
	if cmd.EnvFile != "" {
		args = append([]string{"-l", "-c", `f=$1; shift; . "$f"; ` + shimsFirst + `exec "$@"`, "login", cmd.EnvFile, cmd.Program}, cmd.Args...)
	}
	return ports.ProcessSpec{Path: sh, Args: args, Dir: cmd.Dir, Env: env(cmd)}, nil
}

// PrepareConfined runs the login shell as Prepare does, but the program it
// execs is cmd confined: the profile is sourced outside the confinement and
// only the program, and what it starts, is inside it.
func (l Login) PrepareConfined(ctx context.Context, cmd ports.ShellCommand, confine func(ports.SandboxCommand) (ports.SandboxCommand, error)) (ports.ProcessSpec, error) {
	w, err := confine(ports.SandboxCommand{Path: cmd.Program, Args: slices.Clone(cmd.Args), Dir: cmd.Dir, Env: cmd.Env})
	if err != nil {
		return ports.ProcessSpec{}, err
	}
	cmd.Program, cmd.Args, cmd.Dir, cmd.Env = w.Path, w.Args, w.Dir, w.Env
	return l.Prepare(ctx, cmd)
}

// shimsFirst puts the session's shim dir, if it has one, first on PATH, and
// its keychain shim dir and then its developer bin, if it has them, right
// after it.
const shimsFirst = `PATH="${` + ports.ShimDirEnv + `:+$` + ports.ShimDirEnv + `:}${` + ports.KeychainShimDirEnv + `:+$` + ports.KeychainShimDirEnv + `:}${` + ports.DeveloperBinEnv + `:+$` + ports.DeveloperBinEnv + `:}$PATH"; export PATH; `

func (Login) CheckDir(_ context.Context, dir string) error { return checkDir(dir) }

func env(cmd ports.ShellCommand) []string { return append(os.Environ(), cmd.Env...) }

func checkDir(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "directory " + dir + " does not exist"}
	case err != nil:
		return err
	case !info.IsDir():
		return &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: dir + " is not a directory"}
	}
	return nil
}
