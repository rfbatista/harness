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

// outsideDir makes a scratch directory neither under $HOME nor under the
// system temp dir, the two places the profile denies on its own.
func outsideDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/private/var/tmp", "harness-seatbelt-outside-")
	if err != nil {
		t.Skipf("cannot write outside $HOME and the temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	real, _ := filepath.EvalSymlinks(dir)
	home, _ := os.UserHomeDir()
	temp, _ := filepath.EvalSymlinks(os.TempDir())
	for _, denied := range []string{home, temp} {
		if real == denied || strings.HasPrefix(real, denied+"/") {
			t.Skipf("%s is under %s, which the profile denies anyway", real, denied)
		}
	}
	return real
}

func TestAHiddenPathOutsideHomeAndTempIsNeitherReadNorWrittenButAGrantedPathInsideItIs(t *testing.T) {
	d := requireSeatbelt(t)
	root := outsideDir(t)
	// A workspaces root holding the session's worktree, and a repository
	// checkout elsewhere: neither is under $HOME or the temp dir.
	wsRoot, wt := filepath.Join(root, "ws"), filepath.Join(root, "ws", "r", "wt")
	repo := filepath.Join(root, "repo")
	os.MkdirAll(wt, 0o755)
	os.MkdirAll(repo, 0o755)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("SECRET"), 0o644)
	os.WriteFile(filepath.Join(wsRoot, "other.txt"), []byte("SECRET"), 0o644)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}, Hidden: []string{repo, wsRoot}}

	if out, err := run(t, d, p, `echo mine > `+wt+`/f && cat `+wt+`/f`); err != nil || strings.TrimSpace(out) != "mine" {
		t.Fatalf("own worktree inside the hidden root: %v %q", err, out)
	}
	for name, script := range map[string]string{
		"read a hidden repository":  `cat ` + repo + `/a.txt`,
		"write a hidden repository": `echo x > ` + repo + `/b.txt`,
		"read the hidden root":      `cat ` + wsRoot + `/other.txt`,
		"write the hidden root":     `echo x > ` + wsRoot + `/c.txt`,
	} {
		if out, err := run(t, d, p, script); err == nil || strings.Contains(out, "SECRET") {
			t.Errorf("%s: %v %q", name, err, out)
		}
	}
	for _, f := range []string{filepath.Join(repo, "b.txt"), filepath.Join(wsRoot, "c.txt")} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s was written", f)
		}
	}
	// Without Hidden the same paths are open: the deny is what closes them.
	open := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}}
	if out, err := run(t, d, open, `cat `+repo+`/a.txt`); err != nil || out != "SECRET" {
		t.Fatalf("control: %v %q", err, out)
	}
}

func TestADenialOfAHiddenRepositoryIsCaptured(t *testing.T) {
	d := requireSeatbelt(t)
	root := outsideDir(t)
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	os.MkdirAll(repo, 0o755)
	os.MkdirAll(wt, 0o755)
	secret := filepath.Join(repo, "a.txt")
	os.WriteFile(secret, []byte("SECRET"), 0o644)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}, Hidden: []string{repo}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd, err := d.Wrap(p, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", "sleep 3; cat " + secret + "; sleep 3"}, Dir: wt, Env: gitFreeEnv()})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	c.Dir, c.Env = cmd.Dir, cmd.Env
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Wait()
	ch, err := NewCollector().Follow(ctx, wt)
	if err != nil {
		t.Skipf("unified log not available: %v", err)
	}
	if got := waitViolation(t, ch, secret); got.Operation != "file-read-data" {
		t.Fatalf("violation %+v", got)
	}
}

// gitHome is a stand-in home under the real one, with a git config the
// policy reads and an ssh key it does not; the person's own files are never
// read. The driver treats it as the home.
func gitHome(t *testing.T) (*Driver, string, ports.SandboxPolicy) {
	t.Helper()
	d := requireSeatbelt(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	home := homeDir(t)
	d.Home = func() (string, error) { return home, nil }
	wt := filepath.Join(home, "wt")
	os.MkdirAll(wt, 0o755)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Stand In\n\temail = stand@in.test\n[commit]\n\tgpgsign = false\n"), 0o644)
	os.WriteFile(filepath.Join(home, ".ssh", "id_rsa"), []byte("secret"), 0o600)
	p := ports.SandboxPolicy{WorkingDir: wt, Writable: []string{wt}, Readable: []string{filepath.Join(home, ".gitconfig"), filepath.Join(home, ".config", "git")}}
	return d, home, p
}

func TestGitInsideReadsTheStandInGitConfigWithoutXcrunErrorsAndStillCannotReadSSH(t *testing.T) {
	d, home, p := gitHome(t)
	out, err := run(t, d, p, `unset GIT_CONFIG_GLOBAL; git init -q . && git config user.name && echo hi > f && git add f && git status --short && git commit -q -m one && git log --oneline | wc -l`, "HOME="+home)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "Stand In\n") {
		t.Errorf("user.name not read from the stand-in .gitconfig: %q", out)
	}
	if strings.Contains(out, "xcrun") || strings.Contains(out, "not permitted") {
		t.Errorf("git printed an error: %q", out)
	}
	if out, err := run(t, d, p, `cat `+filepath.Join(home, ".ssh", "id_rsa")); err == nil {
		t.Errorf("read the stand-in ssh key: %s", out)
	}
}

// Only the cache files of Apple's git and xcrun are let through the system
// temp dir; any other file in it stays denied, for reading and writing.
func TestOnlyAppleGitCacheFilesAreOpenInTheSystemTempDir(t *testing.T) {
	d, _, p := gitHome(t)
	tmp := os.TempDir()
	other, err := os.CreateTemp(tmp, "harness-seatbelt-other-")
	if err != nil {
		t.Skipf("cannot write to the system temp dir: %v", err)
	}
	other.WriteString("data")
	other.Close()
	t.Cleanup(func() { os.Remove(other.Name()) })
	if out, err := run(t, d, p, `cat `+other.Name()); err == nil {
		t.Errorf("read a non-xcrun file in the system temp dir: %s", out)
	}
	if out, err := run(t, d, p, `echo x > `+filepath.Join(tmp, "harness-seatbelt-new-"+filepath.Base(p.WorkingDir))); err == nil {
		t.Errorf("created a non-xcrun file in the system temp dir: %s", out)
	}
	for _, name := range []string{"xcrun_db-a.b", "xcrun_db_other", "xcrun_dbx"} {
		path := filepath.Join(tmp, name)
		if out, err := run(t, d, p, `echo x > `+path); err == nil {
			os.Remove(path)
			t.Errorf("wrote %s", name)
			_ = out
		}
	}
	probe := filepath.Join(tmp, "xcrun_db-harnessT0")
	t.Cleanup(func() { os.Remove(probe) })
	if out, err := run(t, d, p, `echo x > `+probe); err != nil {
		t.Errorf("could not write an xcrun cache file: %v %s", err, out)
	}
}
