// Package personshell is the ports.Launcher that starts a person's terminal
// (ADR 0052): their login $SHELL, as them, on a PTY, with the environment it
// is given and none of the agent's runtime — no agent sandbox, no edit proxy,
// no agent Claude home.
package personshell

import (
	"context"
	"os"
	"os/exec"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Kind is the AgentSpec.Kind of a person's terminal.
const Kind = ports.PersonTerminalKind

var _ ports.Launcher = (*Launcher)(nil)

// Launcher runs a person's shell.
type Launcher struct {
	pty ports.PTY
	// env is the environment a shell starts with, read at each start.
	env func() []string
	// deny makes a path read-only for the shell; nil on a platform with no
	// driver.
	deny ports.DenyWritesDriver
	// shell is the program to run; empty means $SHELL, else /bin/sh.
	shell string
}

// New returns a launcher on pty; env answers the complete environment of a
// new shell. deny, when set, makes the path a spec names read-only; without
// one such a spec is refused.
func New(pty ports.PTY, env func() []string, deny ports.DenyWritesDriver) *Launcher {
	return &Launcher{pty: pty, env: env, deny: deny}
}

func (*Launcher) Kind() string { return Kind }

// Launch starts `$SHELL -l` in spec.Dir with spec.Env added to the launcher's
// environment. The shell is not the Shell port's: it never wraps the program.
func (l *Launcher) Launch(ctx context.Context, spec ports.AgentSpec, size ports.TermSize, sh ports.Shell) (ports.PTYProcess, error) {
	if err := sh.CheckDir(ctx, spec.Dir); err != nil {
		return nil, err
	}
	prog := l.shell
	if prog == "" {
		prog = os.Getenv("SHELL")
	}
	if prog == "" {
		prog = "/bin/sh"
	}
	path, err := exec.LookPath(prog)
	if err != nil {
		return nil, &domain.StructuredError{Code: "AGENT_CLI_NOT_FOUND", Message: prog + " not found"}
	}
	proc := ports.ProcessSpec{Path: path, Args: []string{"-l"}, Dir: spec.Dir, Env: append(l.env(), spec.Env...)}
	if spec.DenyWrites != "" {
		if l.deny == nil {
			return nil, &domain.StructuredError{Code: "TERMINAL_READ_ONLY_UNAVAILABLE", Message: "This platform has no sandbox that can make the folder read-only, so no terminal opens on it."}
		}
		w, err := l.deny.WrapDenyWrites(spec.DenyWrites, ports.SandboxCommand{Path: proc.Path, Args: proc.Args, Dir: proc.Dir, Env: proc.Env})
		if err != nil {
			return nil, err
		}
		proc.Path, proc.Args, proc.Dir, proc.Env = w.Path, w.Args, w.Dir, w.Env
	}
	return l.pty.Start(proc, size)
}
