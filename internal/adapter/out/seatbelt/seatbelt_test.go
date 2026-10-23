package seatbelt

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

func requireSeatbelt(t *testing.T) *Driver {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not installed")
	}
	return New()
}

// homeDir makes a scratch directory under $HOME, where the sandbox denies
// everything outside its policy.
func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir, err := os.MkdirTemp(home, ".harness-seatbelt-test-")
	if err != nil {
		t.Skipf("cannot write under $HOME: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func run(t *testing.T, d *Driver, p ports.SandboxPolicy, script string, env ...string) (string, error) {
	t.Helper()
	cmd, err := d.Wrap(p, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", script}, Dir: p.WorkingDir, Env: append(gitFreeEnv(), env...)})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command(cmd.Path, cmd.Args...)
	c.Dir, c.Env = cmd.Dir, cmd.Env
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err = c.Run()
	return out.String(), err
}

func TestAProcessWritesItsWorktreeAndCommitsIntoAWorktreeOfAnAllowedGitDir(t *testing.T) {
	d := requireSeatbelt(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := homeDir(t)
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	g := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(repo, 0o755)
	g(repo, "init", "-q")
	g(repo, "commit", "-q", "--allow-empty", "-m", "init")
	g(repo, "worktree", "add", "-q", "-b", "feat", wt)

	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt, filepath.Join(repo, ".git")}, Readable: []string{"/nonexistent"}}
	out, err := run(t, d, p, `echo hi > file.txt && git add file.txt && git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m work 2>/dev/null && git log --oneline 2>/dev/null | wc -l`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "2") {
		t.Fatalf("log: %q", out)
	}
	// The repository's own working tree is not in the policy.
	if out, err := run(t, d, p, `echo x > `+filepath.Join(repo, "x.txt")); err == nil {
		t.Fatalf("wrote the main checkout: %s", out)
	}
}

func TestAnAllowedRepositoryIsReadableButNotWritable(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	repo, wt := filepath.Join(root, "other"), filepath.Join(root, "wt")
	os.MkdirAll(repo, 0o755)
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("contents"), 0o644)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}, Readable: []string{repo}}
	out, err := run(t, d, p, `cat `+repo+`/a.txt`)
	if err != nil || out != "contents" {
		t.Fatalf("read: %v %q", err, out)
	}
	if out, err := run(t, d, p, `echo y >> `+repo+`/a.txt`); err == nil {
		t.Fatalf("wrote a read-only repository: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "contents" {
		t.Fatalf("file changed: %q", b)
	}
}

func TestADeniedPathUnderHomeIsUnreadableAndTheDenialIsCaptured(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	// Stand-ins for ~/.ssh and ~/.claude: same place, never the real secrets.
	secret := filepath.Join(root, ".ssh", "id_rsa")
	os.MkdirAll(filepath.Dir(secret), 0o700)
	os.WriteFile(secret, []byte("SECRET"), 0o600)
	wt := filepath.Join(root, "wt")
	os.MkdirAll(wt, 0o755)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// The session's process: a shell that waits for the collector, then reads
	// through a child.
	cmd, err := d.Wrap(p, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", "sleep 3; cat " + secret + "; sleep 3"}, Dir: wt, Env: gitFreeEnv()})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	c.Dir, c.Env = cmd.Dir, cmd.Env
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Wait()
	ch, err := NewCollector().Follow(ctx, wt)
	if err != nil {
		t.Skipf("unified log not available: %v", err)
	}
	got := waitViolation(t, ch, secret)
	if got.Operation != "file-read-data" || got.Process != "cat" || got.Path != secret || got.When.IsZero() {
		t.Fatalf("violation %+v", got)
	}
	if strings.Contains(out.String(), "SECRET") {
		t.Fatal("secret leaked")
	}
}

func waitViolation(t *testing.T, ch <-chan ports.SandboxViolation, path string) ports.SandboxViolation {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				t.Fatal("collector closed")
			}
			if v.Path == path {
				return v
			}
		case <-timeout:
			t.Fatalf("no violation for %s", path)
		}
	}
}

func TestTheNetworkIsNotBlocked(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("pong"))
			c.Close()
		}
	}()
	p := ports.SandboxPolicy{WorkingDir: root, Writable: []string{root}}
	out, err := run(t, d, p, `/usr/bin/nc -w 3 `+strings.Replace(l.Addr().String(), ":", " ", 1))
	if err != nil || !strings.Contains(out, "pong") {
		t.Fatalf("%v %q", err, out)
	}
}

func TestChildProcessesAreConfinedByTheSamePolicy(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	secret := filepath.Join(root, "secret.txt")
	os.WriteFile(secret, []byte("SECRET"), 0o600)
	wt := filepath.Join(root, "wt")
	os.MkdirAll(wt, 0o755)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}}
	out, err := run(t, d, p, `/bin/sh -c '/bin/sh -c "cat `+secret+`"'`)
	if err == nil || strings.Contains(out, "SECRET") {
		t.Fatalf("grandchild read the secret: %v %q", err, out)
	}
}

func TestPolicyPathsWithSpacesAndQuotesCannotBreakOutOfTheProfile(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	secret := filepath.Join(root, "secret.txt")
	os.WriteFile(secret, []byte("SECRET"), 0o600)
	nasty := filepath.Join(root, `a "b") (allow file-read* (subpath "`+root+`")) ("c`)
	t.Setenv("NASTY", nasty)
	os.MkdirAll(nasty, 0o755)
	spaced := filepath.Join(root, "has space")
	os.MkdirAll(spaced, 0o755)
	p := ports.SandboxPolicy{WorkingDir: spaced, Writable: []string{spaced, nasty}}
	if out, err := run(t, d, p, `echo ok > "has space.txt" && echo ok > "$NASTY/f" && echo ok > f`); err != nil {
		t.Fatalf("allowed paths unusable: %v %s", err, out)
	}
	if out, err := run(t, d, p, `cat `+secret); err == nil || strings.Contains(out, "SECRET") {
		t.Fatalf("injection opened the home: %v %q", err, out)
	}
}

func TestSymlinkedPolicyPathsAreResolved(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	real := filepath.Join(root, "real")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	p := ports.SandboxPolicy{WorkingDir: link, Writable: []string{link}}
	if out, err := run(t, d, p, `echo ok > f && echo ok > `+real+`/g`); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

func TestWrapRefusesAPathItCannotRepresent(t *testing.T) {
	d := New()
	if _, err := d.Wrap(ports.SandboxPolicy{WorkingDir: "/x", Writable: []string{"relative"}}, ports.SandboxCommand{Path: "claude"}); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestWrapKeepsTheCommandLineAndEnv(t *testing.T) {
	d := New()
	cmd, err := d.Wrap(ports.SandboxPolicy{WorkingDir: "/x"}, ports.SandboxCommand{Path: "claude", Args: []string{"--print", "a b"}, Dir: "/x", Env: []string{"A=1"}})
	if err != nil {
		t.Fatal(err)
	}
	n := len(cmd.Args)
	if cmd.Dir != "/x" || cmd.Env[0] != "A=1" || cmd.Args[n-2] != "--print" || cmd.Args[n-1] != "a b" || d.Name() != "seatbelt" {
		t.Fatalf("%+v", cmd)
	}
}

// gitFreeEnv is the environment without anything that points git at the
// harness's own config under the home.
func gitFreeEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") && !strings.HasPrefix(e, "HARNESS_") {
			env = append(env, e)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
}
