package orchestration

import (
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestLoginWrap_ThePolicyIsTheAgentHomeAloneWithItsOwnTempDir(t *testing.T) {
	d := &portstest.RecordingSandboxDriver{}
	home, tmp, claudeDir := t.TempDir(), t.TempDir(), "/opt/claude/versions"
	sb := &Sandbox{
		Driver: d, Home: home,
		UserHome: "/home/p", ConfigHome: "/home/p/.config",
		Checkouts:      func() []string { return []string{"/home/p/work/repo"} },
		WorkspacesRoot: func() string { return "/home/p/workspaces" },
		Allow:          func(string) []ports.SandboxPath { return []ports.SandboxPath{{Path: "/home/p/go", Write: true}} },
		Extra:          func() []ports.SandboxPath { return []ports.SandboxPath{{Path: "/home/p/extra", Write: true}} },
	}
	wrap := sb.LoginWrap([]ports.SandboxPath{{Path: claudeDir}})
	if wrap == nil {
		t.Fatal("no wrap with a driver")
	}
	env := []string{"CLAUDE_CONFIG_DIR=" + home, "TMPDIR=" + tmp}
	if _, err := wrap(ports.SandboxCommand{Path: "/opt/claude/versions/claude", Args: []string{"auth", "login", "--claudeai"}, Dir: home, Env: env}); err != nil {
		t.Fatal(err)
	}
	calls := d.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	p := calls[0].Policy
	if !slices.Contains(p.Writable, home) || !slices.Contains(p.Writable, tmp) {
		t.Errorf("writable = %v, want the agent home and the login's temp dir", p.Writable)
	}
	if !slices.Contains(p.Readable, claudeDir) {
		t.Errorf("readable = %v, want claude's install dir", p.Readable)
	}
	for _, path := range slices.Concat(p.Readable, p.Writable) {
		if strings.Contains(path, "/home/p") || strings.Contains(path, ".ssh") || strings.Contains(path, "Keychains") {
			t.Errorf("policy grants %q: the login sees the agent home alone", path)
		}
	}
	for _, repo := range []string{"/home/p/work/repo", "/home/p/workspaces"} {
		if !slices.Contains(p.Hidden, repo) {
			t.Errorf("hidden = %v, want %s", p.Hidden, repo)
		}
	}
	if calls[0].Command.Env == nil || !slices.Contains(calls[0].Command.Env, "CLAUDE_CONFIG_DIR="+home) {
		t.Errorf("env = %v", calls[0].Command.Env)
	}
}

func TestLoginWrap_WithoutADriverThereIsNoWrap(t *testing.T) {
	if (&Sandbox{}).LoginWrap(nil) != nil || (*Sandbox)(nil).LoginWrap(nil) != nil {
		t.Error("a platform without a driver runs the login unwrapped")
	}
}

func TestLoginWrap_StaysOnWithTheSandboxSwitchedOff(t *testing.T) {
	d := &portstest.RecordingSandboxDriver{}
	sb := &Sandbox{Driver: d, Home: t.TempDir(), Enabled: func() bool { return false }}
	wrap := sb.LoginWrap(nil)
	if wrap == nil {
		t.Fatal("the login is confined whenever the platform has a driver")
	}
	if _, err := wrap(ports.SandboxCommand{Path: "claude", Env: []string{"TMPDIR=" + t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	if len(d.Calls()) != 1 {
		t.Error("the driver was not called")
	}
}
