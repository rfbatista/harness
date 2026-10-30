//go:build darwin

package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// underTheAllowlist runs a command confined by the real driver with the
// built-in allowlist as the only grant beside a temp working dir.
func underTheAllowlist(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	driver := platformSandboxDriver()
	if driver == nil {
		t.Skip("no sandbox driver on this platform")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	policy := ports.SandboxPolicy{WorkingDir: t.TempDir()}
	policy.Writable = append(policy.Writable, policy.WorkingDir)
	for _, p := range sandboxAllowlist(allowSources{Home: home, Getenv: os.Getenv}) {
		if p.Write {
			policy.Writable = append(policy.Writable, p.Path)
		} else {
			policy.Readable = append(policy.Readable, p.Path)
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	wrapped, err := driver.Wrap(policy, ports.SandboxCommand{Path: path, Args: args, Dir: policy.WorkingDir, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wrapped.Path, wrapped.Args...)
	cmd.Dir, cmd.Env = wrapped.Dir, wrapped.Env
	out, err := cmd.CombinedOutput()
	if strings.Contains(string(out), "sandbox_apply: Operation not permitted") {
		t.Skip("already inside a sandbox: Seatbelt does not nest")
	}
	return string(out), err
}

// TestAgentSandbox_UvAndPodmanRunUnderTheRealDriver_EndToEnd checks that the
// built-in allowlist is what stdio MCP servers run by uv or podman need to
// start (ADR 0053).
func TestAgentSandbox_UvAndPodmanRunUnderTheRealDriver_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	t.Run("uv reads its cache dir", func(t *testing.T) {
		if out, err := underTheAllowlist(t, "uv", "cache", "dir"); err != nil {
			t.Fatalf("uv failed under the allowlist: %v\n%s", err, out)
		}
	})
	t.Run("podman reaches its machine", func(t *testing.T) {
		if err := exec.Command("podman", "info").Run(); err != nil {
			t.Skipf("podman or its machine is not running: %v", err)
		}
		if out, err := underTheAllowlist(t, "podman", "info"); err != nil {
			t.Fatalf("podman failed under the allowlist: %v\n%s", err, out)
		}
	})
}
