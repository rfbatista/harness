package claudecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// wrapping is an Interactive that runs the Wrap llmkit would, on a command it
// makes up, and stops there.
type wrapping struct {
	cmd claude.Command
	out claude.Command
	err error
}

func (w *wrapping) StartInteractive(_ context.Context, cfg claude.InteractiveConfig) (claude.Terminal, error) {
	w.out, w.err = cfg.Wrap(w.cmd)
	if w.err != nil {
		return nil, w.err
	}
	return nil, errCaptured
}

type preparedShell struct{}

func (preparedShell) Prepare(_ context.Context, c ports.ShellCommand) (ports.ProcessSpec, error) {
	return ports.ProcessSpec{Path: "/bin/sh", Args: append([]string{"-c", c.Program}, c.Args...), Dir: c.Dir, Env: c.Env}, nil
}
func (preparedShell) CheckDir(context.Context, string) error { return nil }

func sandboxedSpec() ports.AgentSpec {
	return ports.AgentSpec{
		Kind: Kind, SessionID: "s1", Dir: "/work", Env: []string{"A=1"},
		Conversation: ports.Conversation{ID: "c1"},
		Sandbox:      &ports.SandboxPolicy{WorkingDir: "/work", Writable: []string{"/work"}},
	}
}

func TestASandboxedSpecIsConfinedAfterTheShellPreparedItAndWithTheLaunchersOwnPaths(t *testing.T) {
	drv := &portstest.RecordingSandboxDriver{}
	w := &wrapping{cmd: claude.Command{Path: "/opt/claude/bin/claude", Dir: "/work", Env: []string{"A=1"}}}
	l := NewLauncher(w).WithSandbox(drv, ports.SandboxPath{Path: "/agent-home", Write: true}, ports.SandboxPath{Path: "/opt/claude"})
	_, err := l.Launch(context.Background(), sandboxedSpec(), ports.TermSize{Cols: 80, Rows: 24}, preparedShell{})
	if !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	calls := drv.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times", len(calls))
	}
	c := calls[0]
	if c.Command.Path != "/bin/sh" {
		t.Errorf("driver wrapped %q, want the shell's prepared process", c.Command.Path)
	}
	if !slices.Contains(c.Policy.Writable, "/work") || !slices.Contains(c.Policy.Writable, "/agent-home") || !slices.Contains(c.Policy.Readable, "/opt/claude") {
		t.Errorf("policy = %+v, want the spec's paths and the launcher's", c.Policy)
	}
}

func TestASandboxedSpecIsRefusedWithoutADriver(t *testing.T) {
	w := &wrapping{cmd: claude.Command{Path: "claude"}}
	_, err := NewLauncher(w).Launch(context.Background(), sandboxedSpec(), ports.TermSize{Cols: 80, Rows: 24}, preparedShell{})
	if err == nil || !strings.Contains(err.Error(), "sandbox") || errors.Is(err, errCaptured) {
		t.Fatalf("err = %v, want a refusal naming the sandbox, not a start", err)
	}
}

func TestADriverErrorFailsTheLaunch(t *testing.T) {
	drv := &portstest.RecordingSandboxDriver{Err: errors.New("nope")}
	w := &wrapping{cmd: claude.Command{Path: "claude"}}
	_, err := NewLauncher(w).WithSandbox(drv).Launch(context.Background(), sandboxedSpec(), ports.TermSize{Cols: 80, Rows: 24}, preparedShell{})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}

func TestASpecWithoutAPolicyRunsUnconfined(t *testing.T) {
	drv := &portstest.RecordingSandboxDriver{}
	w := &wrapping{cmd: claude.Command{Path: "claude"}}
	spec := sandboxedSpec()
	spec.Sandbox = nil
	if _, err := NewLauncher(w).WithSandbox(drv).Launch(context.Background(), spec, ports.TermSize{}, preparedShell{}); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	if n := len(drv.Calls()); n != 0 {
		t.Fatalf("driver called %d times for an unsandboxed spec", n)
	}
}

func TestTheWorktreeIsTrustedInTheAgentHomeBeforeTheStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "home")
	w := &wrapping{cmd: claude.Command{Path: "claude"}}
	spec := sandboxedSpec()
	spec.Sandbox = nil
	spec.Dir = "/some/worktree"
	if _, err := NewLauncher(w).WithHome(claudehome.NewHome(dir)).Launch(context.Background(), spec, ports.TermSize{}, preparedShell{}); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Onboarded bool `json:"hasCompletedOnboarding"`
		Projects  map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if !m.Onboarded || !m.Projects["/some/worktree"].Trusted {
		t.Fatalf("config = %s, want onboarding done and the worktree trusted", b)
	}
}
