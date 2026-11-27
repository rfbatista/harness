//go:build !windows

package gateruns

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Wrapper confines a command under a session's sandbox profile; nil, or one
// answering a nil wrap, runs it unconfined (the session's sandbox is off).
type Wrapper interface {
	GateWrap(ctx context.Context, sess *domain.Session) (func(ports.SandboxCommand) (ports.SandboxCommand, error), error)
}

// ShellExecutor runs a gate command with sh in the session's worktree, in its
// own process group so a timeout kills what the command started too.
type ShellExecutor struct {
	Sandbox Wrapper
	// Env is the environment the command runs with; nil: the server's.
	Env []string
}

var _ ports.GateExecutor = (*ShellExecutor)(nil)

// Execute implements ports.GateExecutor.
func (e *ShellExecutor) Execute(ctx context.Context, sess *domain.Session, command string, timeout time.Duration, out io.Writer) ports.GateExit {
	start := time.Now()
	fail := func(err error) ports.GateExit {
		_, _ = io.WriteString(out, err.Error()+"\n")
		return ports.GateExit{ExitCode: -1, Duration: time.Since(start)}
	}
	env := e.Env
	if env == nil {
		env = os.Environ()
	}
	cmd := ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", command}, Dir: sess.WorkingDir, Env: env}
	if e.Sandbox != nil {
		wrap, err := e.Sandbox.GateWrap(ctx, sess)
		if err != nil {
			return fail(err)
		}
		if wrap != nil {
			if cmd, err = wrap(cmd); err != nil {
				return fail(err)
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	c.Dir, c.Env = cmd.Dir, cmd.Env
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	// The gate's output goes through a pipe this code owns, so the run is over
	// when the shell exits, not when every process it started lets go of the
	// pipe: a background child must not turn a passing gate red or hold the run.
	pr, pw, perr := os.Pipe()
	if perr != nil {
		return fail(perr)
	}
	c.Stdout, c.Stderr = pw, pw
	if err := c.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return fail(err)
	}
	_ = pw.Close()
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, pr)
		close(copied)
	}()
	err := c.Wait()
	exit := ports.GateExit{Duration: time.Since(start)}
	leftover := ctx.Err() == nil && syscall.Kill(-c.Process.Pid, 0) == nil
	if leftover {
		// The shell is gone but its process group is not: stop what the gate left behind.
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-copied:
	case <-time.After(2 * time.Second):
	}
	_ = pr.Close()
	if leftover {
		_, _ = io.WriteString(out, "\ngate left a background process; it was stopped\n")
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		exit.ExitCode, exit.TimedOut = -1, true
		_, _ = io.WriteString(out, "\ngate timed out after "+timeout.String()+"\n")
	case err == nil:
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit.ExitCode = ee.ExitCode()
		} else {
			return fail(err)
		}
	}
	return exit
}
