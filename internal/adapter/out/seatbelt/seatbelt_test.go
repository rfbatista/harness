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

func TestGitInsideReadsTheStandInGitConfigAndStillCannotReadSSH(t *testing.T) {
	d, home, p := gitHome(t)
	out, err := run(t, d, p, `unset GIT_CONFIG_GLOBAL; git init -q . && git config user.name && echo hi > f && git add f && git status --short && git commit -q -m one && git log --oneline | wc -l`, "HOME="+home, "PATH="+developerBin(t)+":"+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "Stand In\n") {
		t.Errorf("user.name not read from the stand-in .gitconfig: %q", out)
	}
	if out, err := run(t, d, p, `cat `+filepath.Join(home, ".ssh", "id_rsa")); err == nil {
		t.Errorf("read the stand-in ssh key: %s", out)
	}
}

// With the active developer directory's usr/bin first on PATH, git runs
// directly and not through /usr/bin's xcrun shim, so it prints no xcrun
// error even though the shim's shared cache cannot be written.
func TestGitWithTheDeveloperBinFirstPrintsNoXcrunError(t *testing.T) {
	d, home, p := gitHome(t)
	bin := developerBin(t)
	out, err := run(t, d, p, `unset GIT_CONFIG_GLOBAL; git init -q . && echo hi > f && git add f && git status --short && git commit -q -m one && git log --oneline | wc -l`,
		"HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "xcrun") || strings.Contains(out, "not permitted") {
		t.Errorf("git printed an error: %q", out)
	}
}

func developerBin(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil {
		t.Skipf("no developer directory: %v", err)
	}
	return filepath.Join(strings.TrimSpace(string(out)), "usr", "bin")
}

// The shared xcrun cache in the system temp dir is a user-wide file the
// person's own xcrun runs tools from: a session can neither write nor create
// it, nor any file in that dir.
func TestTheSharedXcrunCacheCanNeitherBeWrittenNorCreated(t *testing.T) {
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
		t.Errorf("read a file in the system temp dir: %s", out)
	}
	// Opening for append changes no byte of the person's real cache, but
	// succeeds only when the policy allows writing it.
	for _, name := range []string{"xcrun_db", "xcrun_db-harnessT0", "harness-seatbelt-new"} {
		path := filepath.Join(tmp, name)
		_, statErr := os.Stat(path)
		existed := statErr == nil
		if out, err := run(t, d, p, `: >> `+path); err == nil {
			t.Errorf("wrote %s: %s", name, out)
		}
		if _, err := os.Stat(path); err == nil && !existed {
			os.Remove(path)
			t.Errorf("created %s", name)
		}
	}
}

// worktreeFixture is a real repository with the session's own worktree and
// two others, outside $HOME and the temp dir, and the policy the builder
// makes for the own one: the git dir writable, its worktrees dir read-only,
// the own entry writable.
type worktreeFixture struct {
	repo, own, other, gone string
	git, worktrees         string
	policy                 ports.SandboxPolicy
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := outsideDir(t)
	f := worktreeFixture{repo: filepath.Join(root, "repo"), own: filepath.Join(root, "own"), other: filepath.Join(root, "other"), gone: filepath.Join(root, "gone")}
	g := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = f.repo
		c.Env = gitFreeEnv()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(f.repo, 0o755)
	g("init", "-q")
	g("commit", "-q", "--allow-empty", "-m", "init")
	g("worktree", "add", "-q", "-b", "own", f.own)
	g("worktree", "add", "-q", "-b", "other", f.other)
	g("worktree", "add", "-q", "-b", "gone", f.gone)
	// The gone worktree's checkout is deleted: its admin entry is what a
	// prune would remove.
	if err := os.RemoveAll(f.gone); err != nil {
		t.Fatal(err)
	}
	f.git = filepath.Join(f.repo, ".git")
	f.worktrees = filepath.Join(f.git, "worktrees")
	f.policy = ports.SandboxPolicy{
		WorkingDir: f.own,
		Writable:   []string{f.own, f.git, filepath.Join(f.worktrees, "own")},
		Readable:   []string{f.worktrees},
	}
	return f
}

func TestOtherWorktreesAdminEntriesStayReadOnlyWhileTheOwnWorktreeCommits(t *testing.T) {
	d := requireSeatbelt(t)
	f := newWorktreeFixture(t)

	out, err := run(t, d, f.policy, `echo hi > file.txt && git add file.txt && git -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m work && git log --oneline | wc -l`)
	if err != nil || !strings.HasSuffix(strings.TrimSpace(out), "2") {
		t.Fatalf("commit in the own worktree: %v %q", err, out)
	}

	// Control: without the read-only worktrees dir, prune removes the entry
	// of the worktree whose checkout is gone.
	open := f.policy
	open.Readable = nil
	if out, err := run(t, d, open, `git worktree prune`); err != nil {
		t.Fatalf("control prune: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(f.worktrees, "gone")); err == nil {
		t.Fatal("control: prune left the entry, so the test proves nothing")
	}
}

func TestPruneInTheSandboxLeavesAnOtherWorktreesAdminEntryAndCheckout(t *testing.T) {
	d := requireSeatbelt(t)
	f := newWorktreeFixture(t)
	os.WriteFile(filepath.Join(f.other, "keep.txt"), []byte("keep"), 0o644)

	run(t, d, f.policy, `git worktree prune`)
	for _, p := range []string{filepath.Join(f.worktrees, "gone"), filepath.Join(f.worktrees, "other", "HEAD"), filepath.Join(f.worktrees, "own", "HEAD"), filepath.Join(f.other, "keep.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s is gone: %v", p, err)
		}
	}
	if out, err := run(t, d, f.policy, `echo x > `+filepath.Join(f.worktrees, "other", "HEAD")); err == nil {
		t.Fatalf("wrote another worktree's entry: %s", out)
	}
	if out, err := run(t, d, f.policy, `cat `+filepath.Join(f.worktrees, "other", "HEAD")); err != nil {
		t.Fatalf("could not read another worktree's entry: %v %s", err, out)
	}
}

func TestAWriteToAnOtherWorktreesAdminEntryIsCaptured(t *testing.T) {
	d := requireSeatbelt(t)
	f := newWorktreeFixture(t)
	target := filepath.Join(f.worktrees, "other", "HEAD")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd, err := d.Wrap(f.policy, ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", "sleep 3; echo x > " + target + "; sleep 3"}, Dir: f.own, Env: gitFreeEnv()})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.CommandContext(ctx, cmd.Path, cmd.Args...)
	c.Dir, c.Env = cmd.Dir, cmd.Env
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Wait()
	ch, err := NewCollector().Follow(ctx, f.own)
	if err != nil {
		t.Skipf("unified log not available: %v", err)
	}
	waitViolation(t, ch, target)
}

func TestACreateOnlyDirTakesNewEntriesWhileTheReadOnlyOnesOnTopStayUntouchable(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	dir, old := filepath.Join(root, "dir"), filepath.Join(root, "dir", "old")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "f"), []byte("keep"), 0o644)
	p := ports.SandboxPolicy{WorkingDir: root, CreateOnly: []string{dir}, Readable: []string{old}}

	if out, err := run(t, d, p, `mkdir `+dir+`/new && echo x > `+dir+`/new/f && mv `+dir+`/new `+dir+`/new2 && rm -r `+dir+`/new2`); err != nil {
		t.Fatalf("a new entry is made, changed and removed: %v %s", err, out)
	}
	for name, script := range map[string]string{
		"rewrite": `echo x > ` + old + `/f`, "delete a file": `rm ` + old + `/f`,
		"delete the entry": `rm -r ` + old, "rename the entry": `mv ` + old + ` ` + dir + `/moved`,
	} {
		if out, err := run(t, d, p, script); err == nil {
			t.Errorf("%s worked: %s", name, out)
		}
	}
	if b, err := os.ReadFile(filepath.Join(old, "f")); err != nil || string(b) != "keep" {
		t.Errorf("old entry = %q, %v", b, err)
	}
}

// The commit check's pre-commit runs in the sandbox from the session's own
// hooks dir in the data dir; another session's dir stays closed (ADR 0040).
func TestAProcessExecutesAHookInItsOwnHooksDirButNotInAnotherSessionsDir(t *testing.T) {
	d := requireSeatbelt(t)
	root := homeDir(t)
	work := filepath.Join(root, "work")
	own, other := filepath.Join(root, "githooks", "own"), filepath.Join(root, "githooks", "other")
	for _, dir := range []string{work, own, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte("#!/bin/sh\necho hook-ran\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy := ports.SandboxPolicy{WorkingDir: work, Writable: []string{work}, Readable: []string{own}}

	if out, err := run(t, d, policy, own+"/pre-commit"); err != nil || !strings.Contains(out, "hook-ran") {
		t.Errorf("own hooks dir: %v %q, want the hook to run", err, out)
	}
	out, err := run(t, d, policy, other+"/pre-commit")
	if err == nil || strings.Contains(out, "hook-ran") {
		t.Errorf("another session's hooks dir: %v %q, want it denied", err, out)
	}
	if _, err := run(t, d, policy, "echo x > "+own+"/pre-commit"); err == nil {
		t.Error("the sandbox wrote its own hooks dir, want it read-only")
	}
}
