//go:build linux

package bwrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// realDriver skips where bwrap is absent or cannot make a sandbox here.
func realDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New()
	if err != nil {
		t.Skip("bwrap not installed")
	}
	probe := exec.Command(d.bin, "--ro-bind", "/", "/", "true")
	if err := probe.Run(); err != nil {
		t.Skipf("bwrap cannot create a sandbox here: %v", err)
	}
	return d
}

func run(t *testing.T, d *Driver, p ports.SandboxPolicy, script string) (string, error) {
	t.Helper()
	w, err := d.Wrap(p, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", script}, Dir: p.WorkingDir, Env: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, w.Path, w.Args...)
	c.Dir, c.Env = w.Dir, w.Env
	out, err := c.CombinedOutput()
	return string(out), err
}

func TestRealBwrap_WritesItsWorktreeCommitsReadsButDoesNotWriteARepoAndCannotReadDeniedHome(t *testing.T) {
	d := realDriver(t)
	home := t.TempDir()
	d.home = home
	wt, repo, denied := filepath.Join(home, "wt"), filepath.Join(home, "repo"), filepath.Join(home, "secret")
	for _, p := range []string{wt, repo, denied} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(repo, "r.txt"), []byte("ro"), 0o644)
	os.WriteFile(filepath.Join(denied, "s.txt"), []byte("x"), 0o644)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}, Readable: []string{repo}}

	if out, err := run(t, d, p, "git init -q . && git -c user.name=a -c user.email=a@b commit -q --allow-empty -m x && echo ok > f"); err != nil {
		t.Fatalf("write/commit in worktree: %v\n%s", err, out)
	}
	if out, err := run(t, d, p, "cat "+repo+"/r.txt"); err != nil || strings.TrimSpace(out) != "ro" {
		t.Fatalf("read allowed repo: %v %q", err, out)
	}
	if _, err := run(t, d, p, "echo no > "+repo+"/w.txt"); err == nil {
		t.Fatal("wrote a read-only repository")
	}
	if out, err := run(t, d, p, "cat "+denied+"/s.txt"); err == nil {
		t.Fatalf("read a denied path: %q", out)
	}
}

func TestRealBwrap_TheNetworkIsNotBlocked(t *testing.T) {
	d := realDriver(t)
	d.home = t.TempDir()
	// Same network namespace: the host's interfaces are visible.
	host, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		t.Skip(err)
	}
	out, err := run(t, d, ports.SandboxPolicy{WorkingDir: "/"}, "cat /proc/net/dev")
	if err != nil || out != string(host) && strings.Count(out, ":") < strings.Count(string(host), ":") {
		t.Fatalf("network namespace differs: %v\n%s", err, out)
	}
}
