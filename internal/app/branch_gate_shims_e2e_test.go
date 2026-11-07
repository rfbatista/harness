//go:build !windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/vshell"
)

// The branch gate's git shim, run as a real process the way an agent's Bash
// tool runs it, with real git on a fixture repository (contract "Harness
// server ↔ Agent sessions — branch gate" §1 and §3).

// branchGate is a session started with the shims, and its worktree's git.
type branchGate struct {
	*shellE2E
	shimSession
}

func startBranchGate(t *testing.T) *branchGate { return startBranchGateWith(t, ":memory:") }

// startBranchGateWith is startBranchGate over the database at db: a file keeps
// the requests across a restart.
func startBranchGateWith(t *testing.T, db string) *branchGate {
	t.Helper()
	bin := buildServer(t)
	e := newShellE2EWith(t, db)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	g := &branchGate{shellE2E: e}
	g.shimSession = e.startWithShims(e.task("Branch gate"), bin)
	// A second branch, a tag's commit and a file to check out.
	g.setup("branch other", "branch to-delete", "branch to-rename")
	return g
}

// setup runs real git (no shim) in the worktree.
func (g *branchGate) setup(lines ...string) {
	g.t.Helper()
	for _, l := range lines {
		cmd := exec.Command("/bin/sh", "-c", "git "+l)
		cmd.Dir = g.worktree
		if out, err := cmd.CombinedOutput(); err != nil {
			g.t.Fatalf("git %s: %v\n%s", l, err, out)
		}
	}
}

// git runs a command line in the worktree with the session's PATH.
func (g *branchGate) git(line string) ran { return sh(g.t, g.worktree, g.env, "", line) }

// state is what a refused or allowed command could have changed.
func (g *branchGate) state() string {
	g.t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "git branch --format='%(refname:short)' && git rev-parse --abbrev-ref HEAD && git worktree list")
	cmd.Dir = g.worktree
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("state: %v\n%s", err, out)
	}
	return string(out)
}

const needed = "BRANCH_REQUEST_NEEDED: run \"git "

func TestEveryCreateSwitchRenameAndDeleteFormIsRefusedWithoutAGrant(t *testing.T) {
	g := startBranchGate(t)
	for _, line := range []string{
		// create
		"git checkout -b feature-x", "git checkout -B feature-x", "git switch -c feature-x", "git switch -C feature-x",
		"git branch feature-x", "git branch feature-x other", "git checkout -b feature-x other",
		"git worktree add -b feature-x ../wt-x", "git stash branch feature-x",
		// switch
		"git checkout other", "git switch other", "git checkout --detach other", "git switch --detach other", "git checkout $(git rev-parse HEAD)",
		"git worktree add ../wt-other other", "git rebase main other",
		// rename and delete
		"git branch -m to-rename renamed", "git branch -M to-rename renamed", "git branch -d to-delete", "git branch -D to-delete",
		"git branch -D to-delete other",
	} {
		before := g.state()
		r := g.git(line)
		if r.code != 1 || !strings.Contains(r.stderr, needed) {
			t.Errorf("%s: %+v, want exit 1 with %s…", line, r, needed)
		}
		if after := g.state(); after != before {
			t.Errorf("%s changed the repository:\n%s\nto\n%s", line, before, after)
		}
	}
}

func TestACommitOfAScriptOrMakeIsRefusedToo(t *testing.T) {
	g := startBranchGate(t)
	write(t, g.worktree+"/script.sh", "#!/bin/sh\ngit checkout -b from-script\n")
	write(t, g.worktree+"/Makefile", "branch:\n\tgit checkout -b from-make\n")
	for _, line := range []string{"sh script.sh", "make branch"} {
		if r := g.git(line); r.code == 0 || !strings.Contains(r.stderr, needed) {
			t.Errorf("%s: %+v", line, r)
		}
	}
	if strings.Contains(g.state(), "from-") {
		t.Error("a script made a branch")
	}
}

func TestReadsCommitsAndTheSessionBranchAreNeverGated(t *testing.T) {
	g := startBranchGate(t)
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	write(t, g.worktree+"/a.txt", "a\n")
	for _, line := range []string{
		"git status", "git log --oneline", "git diff", "git show HEAD", "git fetch --all", "git rev-parse HEAD",
		"git config user.name", "git config --get user.name", "git config --list", "git branch", "git branch --list", "git branch -a",
		"git branch --show-current", "git branch -vv",
		"git checkout -- a.txt", "git checkout HEAD -- a.txt", "git restore a.txt", "git restore --staged a.txt",
		"git add a.txt", "git -c user.email=a@b -c user.name=n commit -m one", "git merge main", "git checkout " + session, "git switch " + session,
		"git rebase main", "git worktree list", "git stash", "git stash pop", "git stash list",
		"git remote add origin2 https://example.com/x.git", "git remote -v",
	} {
		if r := g.git(line); strings.Contains(r.stderr, "BRANCH_") {
			t.Errorf("%s was gated: %+v", line, r)
		}
	}
	if !strings.Contains(g.git("git log --oneline").stdout, "one") {
		t.Error("the commit did not run")
	}
}

func TestAnUnreachableHarnessRefusesABranchActionAndRunsEverythingElse(t *testing.T) {
	g := startBranchGate(t)
	down := append(append([]string{}, g.env...), "HARNESS_BRANCH_GATE_URL=http://127.0.0.1:1/api/sessions/x/branch_gate")
	r := sh(t, g.worktree, down, "", "git checkout -b feature-x")
	if r.code != 1 || !strings.Contains(r.stderr, "BRANCH_GATE_UNAVAILABLE: the harness could not be reached; the branch action was not made. Try again later.") {
		t.Errorf("%+v", r)
	}
	if strings.Contains(g.state(), "feature-x") {
		t.Error("the branch was made")
	}
	if r := sh(t, g.worktree, down, "", "git status"); r.code != 0 {
		t.Errorf("git status: %+v", r)
	}
}

func TestASessionWithNoVirtualShellHasTheGitShimToo(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	e.orch.ShimBinary = bin
	e.orch.Sandbox = nil // the test itself may run in a sandbox, which cannot nest
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		t.Fatal(err)
	}
	// A headless session with no task has no virtual shell; its claude here
	// runs git the way the Bash tool would.
	out := filepath.Join(t.TempDir(), "out")
	script := "#!/bin/sh\ngit checkout -b feature-x >" + out + " 2>&1\necho \"exit $?\" >>" + out + "\n"
	if err := os.WriteFile(e.cfg.ClaudeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orch.Start(e.ctx, orchestration.StartRequest{ProjectID: e.project, RepositoryID: e.repo, Task: "no task"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the headless session's git", func() error {
		b, _ := os.ReadFile(out)
		if !strings.Contains(string(b), needed) || !strings.Contains(string(b), "exit 1") {
			return fmt.Errorf("output %q", b)
		}
		return nil
	})
}

func TestThePersonsTerminalIsNeverGated(t *testing.T) {
	g := startBranchGate(t)
	// The person's terminal runs with the session's environment stripped of every harness entry.
	env := vshell.PersonEnv(append(os.Environ(), g.env...))
	cmd := exec.Command("/bin/sh", "-c", "git checkout -b person-made && git branch --show-current")
	cmd.Dir = g.worktree
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "person-made") {
		t.Errorf("the person's git: %v\n%s", err, out)
	}
}
