//go:build !windows

package app

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// An agent's git push waits for the person (contract "Harness server ↔ Agent
// sessions — branch gate" §1 push and refused rows; ADR 0071), with the real
// shim, real git and a bare remote.

// withRemote adds a bare repository as remote name of the session's worktree
// and returns its path.
func (g *branchGate) withRemote(name string) string {
	g.t.Helper()
	bare := filepath.Join(g.t.TempDir(), name+".git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		g.t.Fatalf("init bare: %v %s", err, out)
	}
	g.setup("remote add " + name + " " + bare)
	return bare
}

// remoteHas is the commit the bare repository holds for branch, "" when none.
func remoteHas(bare, branch string) string {
	out, err := exec.Command("git", "-C", bare, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (g *branchGate) head() string { return strings.TrimSpace(g.git("git rev-parse HEAD").stdout) }

func TestAPushIsHeldAndOnceApprovedRunsAndIsRecordedAgainstItsRequest(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one")

	commits := func() int {
		n, _ := strconv.Atoi(strings.TrimSpace(g.git("git rev-list --count HEAD").stdout))
		return n
	}()
	hook := g.askHook("git push origin "+session, "Publish the work")
	req := g.pending(1)[0]
	waits(t, hook)
	if req.Action != domain.BranchPush || req.Branch != session || req.Via != "git" || req.Reason != "Publish the work" ||
		req.Remote == nil || req.Remote.Name != "origin" || req.Remote.URL != origin ||
		req.RemoteBranch == nil || *req.RemoteBranch != session || req.Commits == nil || *req.Commits != commits || req.CommitsAgainst == nil || *req.CommitsAgainst != "base" {
		t.Errorf("request = %+v (remote %+v)", req, req.Remote)
	}
	if r := g.git("git push origin " + session); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Fatalf("a push before the answer: %+v", r)
	}
	if got := remoteHas(origin, session); got != "" {
		t.Fatalf("the remote holds %s before the answer", got)
	}
	if code, body := g.answer(req.ID, "approve", ""); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	if a := answered(t, hook); a.decision() != "allow" {
		t.Fatalf("hook answer = %+v", a)
	}

	// The approval covers this push only: another branch asks again.
	if r := g.git("git push origin other"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("another branch with the grant out: %+v", r)
	}
	head := g.head()
	if r := g.git("git push origin " + session); r.code != 0 {
		t.Fatalf("the approved push: %+v", r)
	}
	if got := remoteHas(origin, session); got != head {
		t.Errorf("the remote holds %q, want %s", got, head)
	}
	if used := g.requests("used"); len(used) != 1 || used[0].ID != req.ID {
		t.Errorf("used requests = %+v", used)
	}
	eventually(t, "the push is recorded", func() error {
		got, err := g.pushes.PushesOf(g.ctx, g.id)
		if err != nil || len(got) != 1 {
			return fmt.Errorf("pushes = %+v, %v", got, err)
		}
		p := got[0]
		if p.RequestID != req.ID || p.Branch != session || p.Remote != "origin" || p.RemoteBranch != session || p.Commit != head || p.By != domain.PushByAgent || p.RepositoryID != g.repo {
			return fmt.Errorf("recorded %+v", p)
		}
		return nil
	})
	if r := g.git("git push origin " + session); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("the grant was used twice: %+v", r)
	}
}

func TestAPushToAnotherRemoteOrUnderAnotherNameNeedsItsOwnApproval(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	g.withRemote("mirror")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	hook := g.askHook("git push origin "+session+":review/x", "")
	req := g.pending(1)[0]
	if req.RemoteBranch == nil || *req.RemoteBranch != "review/x" || req.Branch != session {
		t.Fatalf("request = %+v", req)
	}
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	for _, line := range []string{"git push mirror " + session + ":review/x", "git push origin " + session, "git push origin " + session + ":review/y"} {
		if r := g.git(line); r.code != 1 || !strings.Contains(r.stderr, needed) {
			t.Errorf("%s with only origin review/x approved: %+v", line, r)
		}
	}
	if r := g.git("git push origin " + session + ":review/x"); r.code != 0 {
		t.Fatalf("the approved push: %+v", r)
	}
	if remoteHas(origin, "review/x") == "" || remoteHas(origin, session) != "" {
		t.Errorf("the branch did not land under the name asked for")
	}
}

func TestAPushTheRemoteRefusesFailsWithGitsReasonAndNothingIsForced(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one", "push -q origin "+session)
	// Another clone moves the remote branch on; the session then commits too.
	other := filepath.Join(t.TempDir(), "other")
	for _, args := range [][]string{
		{"clone", "-q", origin, other},
		{"-C", other, "checkout", "-q", "-B", session, "origin/" + session},
		{"-C", other, "-c", "user.name=x", "-c", "user.email=x@x", "commit", "-q", "--allow-empty", "-m", "remote"},
		{"-C", other, "push", "-q", "origin", "HEAD:refs/heads/" + session},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	before := remoteHas(origin, session)
	g.setup("commit -q --allow-empty -m local")

	hook := g.askHook("git push origin "+session, "")
	req := g.pending(1)[0]
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	r := g.git("git push origin " + session)
	if r.code == 0 || !strings.Contains(strings.ToLower(r.stderr), "rejected") {
		t.Errorf("a push behind the remote: %+v, want git's refusal", r)
	}
	if got := remoteHas(origin, session); got != before {
		t.Errorf("the remote moved from %s to %s: the push was forced", before, got)
	}
}

func TestEveryForcePushAndRemoteBranchDeletionIsRefusedAtOnceWithNoRequest(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one", "push -q origin "+session)
	for _, line := range []string{
		"git push --force origin " + session, "git push -f origin " + session, "git push --force-with-lease origin " + session,
		"git push --force-with-lease=" + session + " origin " + session, "git push --force-if-includes origin " + session,
		"git push origin +" + session, "git push origin +" + session + ":other", "git push --delete origin " + session,
		"git push -d origin " + session, "git push origin :" + session, "git push --mirror origin", "git push --prune origin",
		"git push --all origin", "git push --tags origin", "git push --follow-tags origin",
		// bundled short flags, abbreviated long options and a one-off config override
		"git push -fu origin " + session, "git push -uf origin " + session, "git push -df origin " + session,
		"git push --forc origin " + session, "git push --force-with origin " + session, "git push --del origin " + session,
		"git -c remote.origin.url=/elsewhere push origin " + session, "git -c remote.origin.pushurl=/elsewhere push origin " + session,
	} {
		a := answered(t, g.askHook(line, ""))
		if a.decision() != "deny" || !strings.Contains(a.body, "REFUSED") {
			t.Errorf("%s: hook answered %+v, want a deny with a reason", line, a)
		}
		if r := g.git(line); r.code != 1 || !strings.Contains(r.stderr, "REFUSED") {
			t.Errorf("%s: shim ran %+v, want REFUSED", line, r)
		}
	}
	if n := len(g.requests("")); n != 0 {
		t.Errorf("%d requests filed for refused forms", n)
	}
	if remoteHas(origin, session) == "" {
		t.Error("the remote lost the branch")
	}
}

func TestAFetchIsNeverGatedAndAnUnreachableHarnessRefusesAPush(t *testing.T) {
	g := startBranchGate(t)
	g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	if r := g.git("git fetch origin"); r.code != 0 {
		t.Errorf("fetch: %+v", r)
	}
	if a := answered(t, g.askHook("git fetch origin", "")); a.body != "" {
		t.Errorf("the hook answered a fetch: %+v", a)
	}
	down := append(append([]string{}, g.env...), "HARNESS_BRANCH_GATE_URL=http://127.0.0.1:1/api/sessions/x/branch_gate")
	if r := sh(t, g.worktree, down, "", "git push origin "+session); r.code != 1 || !strings.Contains(r.stderr, "BRANCH_GATE_UNAVAILABLE") {
		t.Errorf("an unreachable harness: %+v", r)
	}
}

func TestADeniedPushReachesNothingAndGivesTheAgentTheNote(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	hook := g.askHook("git push origin "+session, "")
	req := g.pending(1)[0]
	g.answer(req.ID, "deny", "not yet")
	a := answered(t, hook)
	if a.decision() != "deny" || !strings.Contains(a.body, "not yet") || !strings.Contains(a.body, "push "+session+" → origin/"+session) {
		t.Errorf("hook answer = %+v", a)
	}
	if r := g.git("git push origin " + session); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("a denied push ran: %+v", r)
	}
	if remoteHas(origin, session) != "" {
		t.Error("a denied push reached the remote")
	}
}

// git config in the worktree, with no shim.
func (g *branchGate) config(kv ...string) {
	g.t.Helper()
	for i := 0; i+1 < len(kv); i += 2 {
		g.setup("config " + kv[i] + " " + kv[i+1])
	}
}

func TestABarePushIsHeldAsTheDestinationGitWouldPickOrRefusedWhenItCannotBeKnown(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one")
	head := g.head()

	// push.default=upstream sends the branch to its upstream's name.
	g.config("push.default", "upstream", "branch."+session+".remote", "origin", "branch."+session+".merge", "refs/heads/elsewhere")
	hook := g.askHook("git push", "")
	req := g.pending(1)[0]
	if req.Branch != session || req.Remote == nil || req.Remote.Name != "origin" || req.RemoteBranch == nil || *req.RemoteBranch != "elsewhere" {
		t.Fatalf("a bare push under push.default=upstream: %+v (remote %+v, remote branch %v)", req, req.Remote, req.RemoteBranch)
	}
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	if r := g.git("git push"); r.code != 0 {
		t.Fatalf("the approved bare push: %+v", r)
	}
	if remoteHas(origin, "elsewhere") != head || remoteHas(origin, session) != "" {
		t.Errorf("the push did not land where the card said")
	}

	// A destination git picks by a rule the gate cannot read for one branch is refused, asking for the explicit form.
	for _, c := range []struct{ why, config string }{
		{"push.default=matching", "config push.default matching"},
		{"a remote.origin.push refspec", "config push.default simple && git config remote.origin.push 'refs/heads/*:refs/heads/*'"},
		{"push.default=upstream and no upstream", "config push.default upstream"},
	} {
		g.setup("config --unset-all remote.origin.push || true", "config --unset branch."+session+".merge || true", c.config)
		for _, line := range []string{"git push", "git push origin"} {
			a := answered(t, g.askHook(line, ""))
			if a.decision() != "deny" || !strings.Contains(jsonUnescaped(a.body), "git push <remote> <branch>:<remote-branch>") {
				t.Errorf("%s with %s: hook answered %+v, want a deny asking for the explicit form", line, c.why, a)
			}
			if r := g.git(line); r.code != 1 || !strings.Contains(r.stderr, "git push <remote> <branch>:<remote-branch>") {
				t.Errorf("%s with %s: shim ran %+v", line, c.why, r)
			}
		}
	}
	if n := len(g.requests("pending")); n != 0 {
		t.Errorf("%d requests pending for refused pushes", n)
	}
}

func TestAPushToARemoteWithAPushurlIsHeldWithThePushurlAndLandsThere(t *testing.T) {
	g := startBranchGate(t)
	fetchOnly := g.withRemote("origin")
	pushTo := filepath.Join(t.TempDir(), "pushto.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", pushTo).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	g.config("remote.origin.pushurl", pushTo)
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	hook := g.askHook("git push origin "+session, "")
	req := g.pending(1)[0]
	if req.Remote == nil || req.Remote.URL != pushTo {
		t.Fatalf("the card shows %+v, want the pushurl %s (the url is %s)", req.Remote, pushTo, fetchOnly)
	}
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	head := g.head()
	if r := g.git("git push origin " + session); r.code != 0 {
		t.Fatalf("the approved push: %+v", r)
	}
	if remoteHas(pushTo, session) != head || remoteHas(fetchOnly, session) != "" {
		t.Error("the push did not land at the pushurl")
	}
	// Changing the pushurl after the approval asks again.
	g.setup("commit -q --allow-empty -m two")
	hook = g.askHook("git push origin "+session, "")
	g.pending(1)
	g.answer(g.pending(1)[0].ID, "deny", "")
	answered(t, hook)
}

// jsonUnescaped is a hook answer with the < and > that encoding/json escapes put back.
func jsonUnescaped(body string) string {
	return strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(body)
}
