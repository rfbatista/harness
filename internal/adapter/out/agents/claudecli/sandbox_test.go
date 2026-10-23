package claudecli

import (
	"os/exec"
	"reflect"
	"runtime"

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
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/adapter/out/shell"
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

// loginShell is the Login shell with a stand-in shell program.
func loginShell(program string) ports.Shell { return shell.Login{Shell: program} }

func TestWithALoginShellTheDriverWrapsClaudeNotTheShell(t *testing.T) {
	drv := &portstest.RecordingSandboxDriver{}
	w := &wrapping{cmd: claude.Command{Path: "/opt/claude/bin/claude", Args: []string{"--model", "m"}, Dir: "/work", Env: []string{"A=1"}}}
	l := NewLauncher(w).WithSandbox(drv, ports.SandboxPath{Path: "/agent-home", Write: true})
	if _, err := l.Launch(context.Background(), sandboxedSpec(), ports.TermSize{Cols: 80, Rows: 24}, loginShell("/bin/zsh")); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	calls := drv.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times", len(calls))
	}
	if got := calls[0].Command; got.Path != "/opt/claude/bin/claude" || !slices.Equal(got.Args, []string{"--model", "m"}) {
		t.Errorf("driver wrapped %q %q, want the claude command", got.Path, got.Args)
	}
	// The process started is still the login shell, running the claude line.
	if w.out.Path != "/bin/zsh" || !slices.Contains(w.out.Args, "-l") || !slices.Contains(w.out.Args, "/opt/claude/bin/claude") {
		t.Errorf("started %q %q, want the login shell running claude", w.out.Path, w.out.Args)
	}
}

func TestTheLoginShellPolicyIsTheDirectShellPolicy(t *testing.T) {
	policyOf := func(sh ports.Shell) ports.SandboxPolicy {
		drv := &portstest.RecordingSandboxDriver{}
		w := &wrapping{cmd: claude.Command{Path: "/opt/claude/bin/claude", Dir: "/work"}}
		l := NewLauncher(w).WithSandbox(drv, ports.SandboxPath{Path: "/agent-home", Write: true}, ports.SandboxPath{Path: "/opt/claude"})
		if _, err := l.Launch(context.Background(), sandboxedSpec(), ports.TermSize{}, sh); !errors.Is(err, errCaptured) {
			t.Fatal(err)
		}
		return drv.Calls()[0].Policy
	}
	login, direct := policyOf(loginShell("/bin/zsh")), policyOf(preparedShell{})
	if !reflect.DeepEqual(login, direct) {
		t.Fatalf("login policy %+v differs from direct %+v", login, direct)
	}
}

func TestALoginShellSourcesItsProfileOutsideTheSandboxAndOnlyClaudeIsConfined(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not installed")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("no /bin/bash")
	}
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	// A stand-in home under the real one, where the sandbox denies reads.
	home, err := os.MkdirTemp(realHome, ".harness-login-test-")
	if err != nil {
		t.Skipf("cannot write under $HOME: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	home, _ = filepath.EvalSymlinks(home)
	secret := filepath.Join(home, ".ssh", "id_rsa")
	os.MkdirAll(filepath.Dir(secret), 0o700)
	os.WriteFile(secret, []byte("SECRET"), 0o600)

	outside, _ := filepath.EvalSymlinks(t.TempDir())
	bin, wt := filepath.Join(outside, "bin"), filepath.Join(outside, "wt")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(wt, 0o755)
	// The profile reads under home (denied inside the sandbox) and sets a
	// variable and the PATH claude is found on.
	os.WriteFile(filepath.Join(home, "marker"), []byte("m"), 0o600)
	profile := "cat " + filepath.Join(home, "marker") + " >/dev/null || export PROFILE_FAILED=1\n" +
		"export FROM_PROFILE=hello\nexport PATH=" + bin + ":$PATH\n"
	os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0o600)
	out := filepath.Join(outside, "out")
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n{ echo \"var=$FROM_PROFILE failed=$PROFILE_FAILED\"; cat "+secret+" 2>&1; true; } >"+out+"\n"), 0o755)

	drv := seatbelt.New()
	drv.Home = func() (string, error) { return home, nil }
	w := &wrapping{cmd: claude.Command{Path: "claude", Dir: wt, Env: append(os.Environ(), "HOME="+home)}}
	spec := sandboxedSpec()
	spec.Dir = wt
	spec.Sandbox = &ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt, outside}}
	l := NewLauncher(w).WithSandbox(drv, ports.SandboxPath{Path: "/usr"}, ports.SandboxPath{Path: "/bin"}, ports.SandboxPath{Path: "/private/etc"})
	if _, err := l.Launch(context.Background(), spec, ports.TermSize{}, loginShell("/bin/bash")); !errors.Is(err, errCaptured) {
		t.Fatal(err)
	}
	c := exec.Command(w.out.Path, w.out.Args...)
	c.Dir, c.Env = w.out.Dir, w.out.Env
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "var=hello failed=\n") {
		t.Errorf("claude saw %q, want the profile's variable and a profile that read under $HOME", s)
	}
	if strings.Contains(s, "SECRET") || !strings.Contains(s, "Operation not permitted") {
		t.Errorf("claude read the secret: %q", s)
	}
}
