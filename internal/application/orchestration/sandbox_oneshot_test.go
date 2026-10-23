package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func oneShotSandbox(t *testing.T) (*Sandbox, *portstest.RecordingSandboxDriver, string) {
	t.Helper()
	d := &portstest.RecordingSandboxDriver{}
	home := t.TempDir()
	return &Sandbox{Driver: d, Home: home}, d, home
}

func TestOneShotWrap_ThePolicyHoldsNoRepositoryAndOnlyWhatTheOneShotNeeds(t *testing.T) {
	sb, d, home := oneShotSandbox(t)
	claudeDir := "/opt/claude/versions"
	wrap, err := sb.OneShotWrap([]ports.SandboxPath{{Path: claudeDir}})
	if err != nil || wrap == nil {
		t.Fatalf("OneShotWrap = %v, %v; want a wrap", wrap, err)
	}
	sb.Allow = func(string) []ports.SandboxPath { return []ports.SandboxPath{{Path: "/home/p/go", Write: true}} }
	sb.Extra = func() []ports.SandboxPath { return []ports.SandboxPath{{Path: "/home/p/repo", Write: true}} }

	if _, err := wrap(commandIn(os.TempDir())); err != nil {
		t.Fatal(err)
	}
	calls := d.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	p := calls[0].Policy
	if p.WorkingDir != os.TempDir() {
		t.Errorf("WorkingDir = %q, want the temp dir %q", p.WorkingDir, os.TempDir())
	}
	if !slices.Contains(p.Writable, filepath.Clean(os.TempDir())) || !slices.Contains(p.Writable, home) {
		t.Errorf("writable = %v, want the temp dir and the agent home", p.Writable)
	}
	if !slices.Contains(p.Readable, claudeDir) {
		t.Errorf("readable = %v, want claude's install dir", p.Readable)
	}
	for _, path := range slices.Concat(p.Readable, p.Writable) {
		if strings.Contains(path, "/home/p") {
			t.Errorf("policy names %q: a one-shot sees no repository, toolchain or extra path", path)
		}
	}
}

func TestOneShotWrap_ThePolicyAlsoAllowsTheTempDirsResolvedPath(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	wrap, _ := sb.OneShotWrap(nil)
	if _, err := wrap(commandIn(os.TempDir())); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Skip("temp dir does not resolve")
	}
	if p := d.Calls()[0].Policy; !slices.Contains(p.Writable, resolved) {
		t.Errorf("writable = %v, want the resolved temp dir %q", p.Writable, resolved)
	}
}

func TestOneShotWrap_SwitchedOffThereIsNoWrapAndNoDriverCall(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	sb.Enabled = func() bool { return false }
	wrap, err := sb.OneShotWrap(nil)
	if err != nil || wrap != nil {
		t.Fatalf("OneShotWrap = %v, %v; want nil, nil with the sandbox off", wrap, err)
	}
	if n := len(d.Calls()); n != 0 {
		t.Errorf("driver called %d times with the sandbox off", n)
	}
}

func TestOneShotWrap_OnWithoutADriverItIsRefused(t *testing.T) {
	sb, _, _ := oneShotSandbox(t)
	sb.Driver = nil
	if _, err := sb.OneShotWrap(nil); err == nil || !strings.Contains(err.Error(), "sandbox driver") {
		t.Fatalf("err = %v, want the missing-driver refusal", err)
	}
}

func TestOneShotWrap_ADriverErrorIsTheWrapError(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	d.Err = errors.New("cannot confine")
	wrap, _ := sb.OneShotWrap(nil)
	if _, err := wrap(commandIn(os.TempDir())); err == nil || !strings.Contains(err.Error(), "cannot confine") {
		t.Fatalf("err = %v, want the driver's error", err)
	}
}

func TestBuildSandboxPolicy_AOneShotIgnoresEveryRepositoryPath(t *testing.T) {
	p := BuildSandboxPolicy(SandboxSession{OneShot: true, TempDir: "/tmp/x", AgentHome: "/h", Worktree: "/wt", GitCommonDir: "/wt/.git", Repositories: []string{"/r1", "/r2"}, VirtualShell: "/vs"})
	resolved, _ := filepath.EvalSymlinks("/tmp/x")
	for _, path := range slices.Concat(p.Readable, p.Writable) {
		if path != "/tmp/x" && path != "/h" && path != resolved {
			t.Errorf("one-shot policy names %q", path)
		}
	}
	if p.WorkingDir != "/tmp/x" {
		t.Errorf("WorkingDir = %q", p.WorkingDir)
	}
}

func commandIn(dir string) llmkit.Command {
	return llmkit.Command{Path: "/opt/claude/bin/claude", Args: []string{"--print"}, Dir: dir, Env: []string{"A=b"}}
}
