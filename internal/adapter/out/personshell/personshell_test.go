package personshell

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/ports"
)

type capturePTY struct{ started ports.ProcessSpec }

func (c *capturePTY) Start(spec ports.ProcessSpec, _ ports.TermSize) (ports.PTYProcess, error) {
	c.started = spec
	return nil, nil
}

type fakeDriver struct{ path string }

func (f *fakeDriver) WrapDenyWrites(path string, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	f.path = path
	return ports.SandboxCommand{Path: "wrapper", Args: append([]string{"--deny", path, "--", cmd.Path}, cmd.Args...), Dir: cmd.Dir, Env: cmd.Env}, nil
}

func launch(t *testing.T, l *Launcher, spec ports.AgentSpec) error {
	t.Helper()
	spec.Dir = t.TempDir()
	_, err := l.Launch(context.Background(), spec, ports.TermSize{Cols: 80, Rows: 24}, shell.Direct{})
	return err
}

func TestAShellOnTheWorktreeRunsUnwrappedAndWithoutAProfile(t *testing.T) {
	pty := &capturePTY{}
	l := New(pty, func() []string { return []string{"A=1"} }, nil)
	l.shell = "/bin/sh"
	if err := launch(t, l, ports.AgentSpec{}); err != nil {
		t.Fatal(err)
	}
	if pty.started.Path != "/bin/sh" || len(pty.started.Args) != 1 || pty.started.Args[0] != "-l" || len(pty.started.Env) != 1 {
		t.Errorf("started = %+v, want /bin/sh -l with the given env only", pty.started)
	}
}

func TestAShellOnTheResourcesRootRunsUnderTheDenyWritesProfile(t *testing.T) {
	pty, drv := &capturePTY{}, &fakeDriver{}
	l := New(pty, func() []string { return nil }, drv)
	l.shell = "/bin/sh"
	if err := launch(t, l, ports.AgentSpec{DenyWrites: "/res"}); err != nil {
		t.Fatal(err)
	}
	if drv.path != "/res" || pty.started.Path != "wrapper" || pty.started.Args[len(pty.started.Args)-2] != "/bin/sh" || pty.started.Args[len(pty.started.Args)-1] != "-l" {
		t.Errorf("started = %+v, profile path %q", pty.started, drv.path)
	}
}

func TestWithoutADriverAReadOnlyShellIsRefused(t *testing.T) {
	pty := &capturePTY{}
	l := New(pty, func() []string { return nil }, nil)
	l.shell = "/bin/sh"
	err := launch(t, l, ports.AgentSpec{DenyWrites: "/res"})
	if errs.Code(err) != "TERMINAL_READ_ONLY_UNAVAILABLE" {
		t.Errorf("err = %v, want TERMINAL_READ_ONLY_UNAVAILABLE", err)
	}
	if pty.started.Path != "" {
		t.Error("a shell started without its profile")
	}
}
