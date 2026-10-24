package orchestration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// seatbeltFixture is two repositories where the module of one replaces into
// the other, a third outside the home, two linked worktrees of the first (the
// session's own and another session's) and the policy an implement session
// gets for them.
type seatbeltFixture struct {
	driver                    *seatbelt.Driver
	root                      string
	repoA, repoB, repoC       string
	own, other, gone, ws, tmp string
	goroot                    string
	session                   SandboxSession
}

func gitEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") && !strings.HasPrefix(e, "HARNESS_") {
			env = append(env, e)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
}

func newSeatbeltFixture(t *testing.T) *seatbeltFixture {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS only")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not installed")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain")
	}
	out, err := exec.Command(goBin, "env", "GOROOT").Output()
	if err != nil {
		t.Skipf("go env GOROOT: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	f := &seatbeltFixture{driver: seatbelt.New(), goroot: strings.TrimSpace(string(out))}
	mk := func(parent, pattern string) string {
		dir, err := os.MkdirTemp(parent, pattern)
		if err != nil {
			t.Skipf("cannot make a directory in %s: %v", parent, err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		real, _ := filepath.EvalSymlinks(dir)
		return real
	}
	f.root = mk(home, ".harness-readall-test-")
	f.repoC = mk("/private/var/tmp", "harness-readall-outside-")
	f.tmp = mk(os.TempDir(), "harness-readall-session-")
	f.repoA, f.repoB = filepath.Join(f.root, "a"), filepath.Join(f.root, "b")
	f.ws = filepath.Join(f.root, "ws")
	f.own, f.other, f.gone = filepath.Join(f.ws, "own"), filepath.Join(f.ws, "other"), filepath.Join(f.ws, "gone")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(f.repoB, "go.mod"), "module example.com/b\n\ngo 1.21\n")
	write(filepath.Join(f.repoB, "b.go"), "package b\n\nfunc Name() string { return \"b\" }\n")
	write(filepath.Join(f.repoC, "note.txt"), "outside\n")
	write(filepath.Join(f.repoA, "go.mod"), "module example.com/a\n\ngo 1.21\n\nrequire example.com/b v0.0.0\n\nreplace example.com/b => "+f.repoB+"\n")
	write(filepath.Join(f.repoA, "main.go"), "package main\n\nimport \"example.com/b\"\n\nfunc main() { println(b.Name()) }\n")
	g := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir, c.Env = dir, gitEnv()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g(f.repoA, "init", "-q")
	g(f.repoA, "add", ".")
	g(f.repoA, "commit", "-q", "-m", "init")
	for _, wt := range []string{f.own, f.other, f.gone} {
		g(f.repoA, "worktree", "add", "-q", "-b", filepath.Base(wt), wt)
	}
	write(filepath.Join(f.other, "secret.txt"), "another session's work\n")
	if err := os.RemoveAll(f.gone); err != nil { // its admin entry is what a prune would remove
		t.Fatal(err)
	}

	f.session = SandboxSession{
		Mode: domain.SessionModeImplement, Worktree: f.own, GitCommonDir: filepath.Join(f.repoA, ".git"),
		OwnGitDir:    filepath.Join(f.repoA, ".git", "worktrees", "own"),
		Repositories: []string{f.repoA, f.repoB, f.repoC}, WorkspacesRoot: f.ws, TempDir: f.tmp,
		ReadAllRepos: true,
		Allow:        []ports.SandboxPath{{Path: f.goroot}},
	}
	return f
}

// run runs script in the own worktree under the policy the builder gives the
// fixture's session.
func (f *seatbeltFixture) run(t *testing.T, readAll bool, script string) (string, error) {
	t.Helper()
	s := f.session
	s.ReadAllRepos = readAll
	cmd := f.wrap(t, s, script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func (f *seatbeltFixture) wrap(t *testing.T, s SandboxSession, script string) *exec.Cmd {
	t.Helper()
	env := append(gitEnv(),
		"PATH="+filepath.Join(f.goroot, "bin")+":/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin",
		"HOME="+f.tmp, "TMPDIR="+f.tmp, "GOENV=off", "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(f.tmp, "gocache"), "GOMODCACHE="+filepath.Join(f.tmp, "gomod"), "GOPATH="+filepath.Join(f.tmp, "gopath"))
	w, err := f.driver.Wrap(BuildSandboxPolicy(s), ports.SandboxCommand{Path: "/bin/sh", Args: []string{"-c", script}, Dir: f.own, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	c := exec.Command(w.Path, w.Args...)
	c.Dir, c.Env = w.Dir, w.Env
	return c
}

func TestSeatbelt_ReadAllReposAModuleReplacingIntoAnotherRepositoryBuildsAndStaysReadOnly(t *testing.T) {
	f := newSeatbeltFixture(t)

	if out, err := f.run(t, true, `go build -o `+f.tmp+`/out ./... && `+f.tmp+`/out`); err != nil {
		t.Fatalf("go build with the setting on: %v\n%s", err, out)
	}

	for name, script := range map[string]string{
		"another repository's checkout":      `echo x > ` + f.repoB + `/hack`,
		"the own repository's checkout":      `echo x > ` + f.repoA + `/hack`,
		"a file of another repository":       `echo x >> ` + f.repoB + `/b.go`,
		"a repository outside the home":      `echo x > ` + f.repoC + `/hack`,
		"another session's worktree":         `echo x > ` + f.other + `/hack`,
		"a file in another session's wt":     `echo x >> ` + f.other + `/secret.txt`,
		"a new file in the own checkout dir": `mkdir ` + f.repoA + `/newdir`,
	} {
		if out, err := f.run(t, true, script); err == nil {
			t.Errorf("wrote %s: %s", name, out)
		}
	}
	for name, path := range map[string]string{
		"another repository's file":     filepath.Join(f.repoB, "b.go"),
		"the own checkout's file":       filepath.Join(f.repoA, "main.go"),
		"a repository outside the home": filepath.Join(f.repoC, "note.txt"),
	} {
		if out, err := f.run(t, true, "cat "+path); err != nil {
			t.Errorf("could not read %s: %v %s", name, err, out)
		}
	}
	for name, script := range map[string]string{
		"another session's worktree":  "cat " + filepath.Join(f.other, "secret.txt"),
		"the workspaces root listing": "ls " + f.ws,
	} {
		if out, err := f.run(t, true, script); err == nil {
			t.Errorf("read %s: %s", name, out)
		}
	}
	// The own worktree and the git directory still take work and commits.
	if out, err := f.run(t, true, `echo hi > file.txt && git add file.txt && git commit -q -m work && git log --oneline | wc -l`); err != nil || !strings.HasSuffix(strings.TrimSpace(out), "2") {
		t.Fatalf("commit in the own worktree: %v %q", err, out)
	}
}

func TestSeatbelt_ReadAllReposOffTheReplaceDirectoryIsUnreadableAndTheBuildFails(t *testing.T) {
	f := newSeatbeltFixture(t)
	out, err := f.run(t, false, `go build -o `+f.tmp+`/out ./...`)
	if err == nil {
		t.Fatalf("go build succeeded with the setting off: %s", out)
	}
	for _, p := range []string{f.repoB, f.repoC} {
		if out, err := f.run(t, false, "cat "+p+"/*"); err == nil {
			t.Errorf("read %s with the setting off: %s", p, out)
		}
	}
}

func TestSeatbelt_ASessionAddsItsOwnWorktreeButCannotTouchTheEntriesThatExist(t *testing.T) {
	f := newSeatbeltFixture(t)
	entries := filepath.Join(f.repoA, ".git", "worktrees")

	newWT := filepath.Join(f.tmp, "newwt")
	out, err := f.run(t, true, `git worktree add -q -b made `+newWT+` && cd `+newWT+` && echo y > f.txt && git add f.txt && git commit -q -m inside && git log --oneline | wc -l && git worktree list`)
	if err != nil || !strings.Contains(out, newWT) {
		t.Fatalf("git worktree add and commit inside it: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(entries, "newwt", "HEAD")); err != nil {
		t.Errorf("the new worktree's entry is missing: %v", err)
	}

	// Every entry that existed is unchanged, by prune or by hand.
	if out, err := f.run(t, true, `git worktree prune`); err != nil {
		t.Logf("prune: %v %s", err, out)
	}
	for _, n := range []string{"gone", "other", "own"} {
		if _, err := os.Stat(filepath.Join(entries, n, "HEAD")); err != nil {
			t.Errorf("entry %s is gone after a prune: %v", n, err)
		}
	}
	for name, script := range map[string]string{
		"deleting an entry":        `rm -rf ` + entries + `/other`,
		"renaming an entry":        `mv ` + entries + `/other ` + entries + `/moved`,
		"rewriting an entry file":  `echo x > ` + entries + `/other/HEAD`,
		"deleting an entry's file": `rm ` + entries + `/other/HEAD`,
	} {
		if out, err := f.run(t, true, script); err == nil {
			t.Errorf("%s worked: %s", name, out)
		}
	}
	if b, err := os.ReadFile(filepath.Join(entries, "other", "HEAD")); err != nil || !strings.HasPrefix(string(b), "ref:") {
		t.Errorf("another worktree's HEAD = %q, %v", b, err)
	}
	if out, err := f.run(t, true, `cat `+entries+`/other/HEAD`); err != nil {
		t.Errorf("could not read another worktree's entry: %v %s", err, out)
	}
}

func TestSeatbelt_ARenameOfAnExistingWorktreeEntryIsCapturedAsAViolation(t *testing.T) {
	f := newSeatbeltFixture(t)
	entries := filepath.Join(f.repoA, ".git", "worktrees")
	target := filepath.Join(entries, "other")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := f.wrap(t, f.session, "sleep 3; mv "+target+" "+entries+"/moved; sleep 3")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	ch, err := seatbelt.NewCollector().Follow(ctx, f.own)
	if err != nil {
		t.Skipf("unified log not available: %v", err)
	}
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				t.Fatal("collector closed")
			}
			if strings.HasPrefix(v.Path, target) {
				if _, err := os.Stat(target); err != nil {
					t.Errorf("the entry is gone: %v", err)
				}
				return
			}
		case <-ctx.Done():
			t.Fatalf("no violation for %s", target)
		}
	}
}
