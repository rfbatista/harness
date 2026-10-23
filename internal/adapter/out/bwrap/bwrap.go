// Package bwrap is the Linux ports.SandboxDriver: it renders a sandbox policy
// into a bubblewrap (bwrap) invocation (ADR 0017). System directories are
// read-only, the home directory is hidden except the policy's paths, and the
// network is shared. Violation collection is not available on Linux.
package bwrap

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"operators-mcp/internal/ports"
)

// ErrUnavailable is why there is no driver: bwrap is not installed.
var ErrUnavailable = errors.New("bubblewrap (bwrap) is not installed")

// systemDirs are the directories bound read-only. Missing ones are skipped.
var systemDirs = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/etc", "/opt", "/var", "/run/systemd/resolve"}

// Driver applies a policy with bwrap.
type Driver struct {
	bin  string
	home string
}

var _ ports.SandboxDriver = (*Driver)(nil)

// New finds bwrap on PATH. Without it the driver is unavailable (nil and
// ErrUnavailable), so the session start refusal applies.
func New() (*Driver, error) { return lookup(exec.LookPath) }

func lookup(find func(string) (string, error)) (*Driver, error) {
	bin, err := find("bwrap")
	if err != nil {
		return nil, ErrUnavailable
	}
	home, _ := os.UserHomeDir()
	return &Driver{bin: bin, home: home}, nil
}

func (d *Driver) Name() string { return "bubblewrap" }

// ViolationsAvailable is false: bwrap reports no denials.
func (d *Driver) ViolationsAvailable() bool { return false }

// Wrap answers bwrap running cmd confined by policy.
func (d *Driver) Wrap(policy ports.SandboxPolicy, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	args := Render(policy, d.home)
	args = append(args, "--", cmd.Path)
	args = append(args, cmd.Args...)
	return ports.SandboxCommand{Path: d.bin, Args: args, Dir: cmd.Dir, Env: cmd.Env}, nil
}

// Render is the bwrap argument list (before the command) for policy, with
// home the directory to hide. Every path is its own argument, never joined
// into a shell string. Pure: it touches no file.
func Render(policy ports.SandboxPolicy, home string) []string {
	args := []string{"--die-with-parent", "--unshare-pid"}
	for _, d := range systemDirs {
		args = append(args, "--ro-bind-try", d, d)
	}
	args = append(args, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp")
	if home != "" {
		args = append(args, "--tmpfs", filepath.Clean(home))
	}
	type bind struct {
		path  string
		write bool
	}
	var binds []bind
	for _, p := range policy.Readable {
		if p != "" {
			binds = append(binds, bind{filepath.Clean(p), false})
		}
	}
	for _, p := range policy.Writable {
		if p != "" {
			binds = append(binds, bind{filepath.Clean(p), true})
		}
	}
	// Parents first, so a nested path is not hidden by the mount over its parent.
	slices.SortStableFunc(binds, func(a, b bind) int {
		return strings.Count(a.path, "/") - strings.Count(b.path, "/")
	})
	for _, b := range binds {
		flag := "--ro-bind-try"
		if b.write {
			flag = "--bind-try"
		}
		args = append(args, flag, b.path, b.path)
	}
	if policy.WorkingDir != "" {
		args = append(args, "--chdir", filepath.Clean(policy.WorkingDir))
	}
	return args
}
