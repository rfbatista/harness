//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/domain"
)

// push_branch, called as the session over its task MCP endpoint (contract
// "Harness server ↔ Agent sessions — branch gate" §4 and §5; ADR 0071).

// pushBranch calls the tool as the session.
func (g *branchGate) pushBranch(args map[string]any) (map[string]any, error) {
	g.t.Helper()
	for _, tl := range sessionTaskTools(g.cat, g.orch, g.plan, g.sessions, nil, g.channel, nil, nil, tooling.WithBranchPush(g.gate)) {
		if tl.Name != "push_branch" {
			continue
		}
		res, err := tl.Handler(tooling.WithSessionID(context.Background(), g.id), args)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(res)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out, nil
	}
	g.t.Fatal("push_branch is not served by the task server")
	return nil, nil
}

// turnWith waits for the session's next turn and checks its start and parts.
func (g *branchGate) turnWith(prefix string, parts ...string) string {
	g.t.Helper()
	var got string
	eventually(g.t, "the turn "+prefix, func() error {
		got = g.nextTurn()
		if !strings.HasPrefix(got, prefix) {
			return fmt.Errorf("turn %q, want it to start with %q", got, prefix)
		}
		return nil
	})
	for _, p := range parts {
		if !strings.Contains(got, p) {
			g.t.Errorf("turn %q lacks %q", got, p)
		}
	}
	return got
}

func TestPushBranchAsksAndOnceApprovedTheHarnessPushesAndTheTurnSaysSo(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one")
	head := g.head()

	out, err := g.pushBranch(map[string]any{"branch": session, "remote": "origin", "remote_branch": "review/x", "reason": "ready for review"})
	if err != nil || out["state"] != "pending" || out["request_id"] == "" {
		t.Fatalf("push_branch = %v, %v", out, err)
	}
	req := g.pending(1)[0]
	if req.ID != out["request_id"] || req.Action != domain.BranchPush || req.Via != "push_branch" || req.Command != "push_branch" || req.Reason != "ready for review" ||
		req.Branch != session || req.Remote == nil || req.Remote.Name != "origin" || req.Remote.URL != origin || req.RemoteBranch == nil || *req.RemoteBranch != "review/x" || req.Commits == nil {
		t.Fatalf("request = %+v (remote %+v)", req, req.Remote)
	}
	if got := g.status(); got != domain.SessionWaitingApproval {
		t.Errorf("status while pending = %s, want waiting_approval", got)
	}
	if remoteHas(origin, "review/x") != "" {
		t.Fatal("pushed before the answer")
	}
	g.answer(req.ID, "approve", "")
	g.turnWith(fmt.Sprintf("[branch request %s · pushed] %s → origin/review/x, ", req.ID, session), " commits.")
	if got := remoteHas(origin, "review/x"); got != head {
		t.Errorf("the remote holds %q under review/x, want %s", got, head)
	}
	if remoteHas(origin, session) != "" {
		t.Error("the branch also landed under its own name")
	}
	if used := g.requests("used"); len(used) != 1 || used[0].ID != req.ID {
		t.Errorf("used requests = %+v", used)
	}
	pushes, err := g.pushes.PushesOf(g.ctx, g.id)
	if err != nil || len(pushes) != 1 || pushes[0].RequestID != req.ID || pushes[0].By != domain.PushByAgent || pushes[0].RemoteBranch != "review/x" || pushes[0].Commit != head {
		t.Errorf("recorded pushes = %+v, %v", pushes, err)
	}
}

func TestPushBranchDefaultsToOriginAndTheBranchsOwnNameAndADenialPushesNothing(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	if _, err := g.pushBranch(map[string]any{"branch": session}); err != nil {
		t.Fatal(err)
	}
	req := g.pending(1)[0]
	if req.Remote == nil || req.Remote.Name != "origin" || req.RemoteBranch == nil || *req.RemoteBranch != session {
		t.Fatalf("defaults: %+v (remote %+v)", req, req.Remote)
	}
	g.answer(req.ID, "deny", "not yet")
	g.turnWith(fmt.Sprintf("[branch request %s · denied] push %s → origin/%s.", req.ID, session, session), "Note: not yet.")
	if remoteHas(origin, session) != "" {
		t.Error("a denied push reached the remote")
	}
}

func TestPushBranchRefusesWhatCannotBePushedWithItsCodeFirst(t *testing.T) {
	g := startBranchGate(t)
	g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"branch": "no-such-branch"}, "BRANCH_NOT_FOUND"},
		{map[string]any{"branch": session, "remote": "nowhere"}, "REMOTE_NOT_FOUND"},
		{map[string]any{}, "INVALID_INPUT"},
		{map[string]any{"branch": session, "remote_branch": "+x"}, "INVALID_INPUT"},
		{map[string]any{"branch": session, "remote_branch": ":x"}, "INVALID_INPUT"},
		{map[string]any{"branch": "+" + session}, "INVALID_INPUT"},
	} {
		_, err := g.pushBranch(c.args)
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%v: %v, want a message starting with %s", c.args, err, c.want)
		}
	}
	if n := len(g.requests("")); n != 0 {
		t.Errorf("%d requests filed for refused calls", n)
	}
}

func TestAPushBranchTheRemoteRefusesOrHasNoCredentialForReportsWhyAsATurn(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one", "push -q origin "+session)
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

	if _, err := g.pushBranch(map[string]any{"branch": session}); err != nil {
		t.Fatal(err)
	}
	g.answer(g.pending(1)[0].ID, "approve", "")
	g.turnWith("[branch request ", "· push failed] PUSH_REJECTED: ")
	if remoteHas(origin, session) != before {
		t.Error("a rejected push moved the remote: it was forced")
	}

	// A host with no stored credential: the credential error names the host.
	g.setup("remote add hosted https://git.example.invalid/x.git")
	if _, err := g.pushBranch(map[string]any{"branch": session, "remote": "hosted"}); err != nil {
		t.Fatal(err)
	}
	g.answer(g.pending(1)[0].ID, "approve", "")
	g.turnWith("[branch request ", "· push failed] GIT_CREDENTIAL_MISSING: ", "git.example.invalid")
}

func TestPushBranchPushesExactlyTheCommitThatWasRequestedAndSaysWhenTheBranchMoved(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	g.setup("commit -q --allow-empty -m one")
	asked := g.head()
	asCount := func() int {
		n, _ := strconv.Atoi(strings.TrimSpace(g.git("git rev-list --count HEAD").stdout))
		return n
	}()

	if _, err := g.pushBranch(map[string]any{"branch": session, "remote_branch": "review/pinned"}); err != nil {
		t.Fatal(err)
	}
	req := g.pending(1)[0]
	if req.Commits == nil || *req.Commits != asCount {
		t.Fatalf("the card counts %v commits, want %d", req.Commits, asCount)
	}
	// The branch moves on before the person answers.
	g.setup("commit -q --allow-empty -m two")
	moved := g.head()
	g.answer(req.ID, "approve", "")
	g.turnWith(fmt.Sprintf("[branch request %s · pushed] %s → origin/review/pinned, %d commits", req.ID, session, asCount), "moved", "approved")
	if got := remoteHas(origin, "review/pinned"); got != asked {
		t.Errorf("the remote holds %q, want the approved commit %s (the branch is at %s)", got, asked, moved)
	}
	pushes, err := g.pushes.PushesOf(g.ctx, g.id)
	if err != nil || len(pushes) != 1 || pushes[0].Commit != asked {
		t.Errorf("recorded pushes = %+v, %v; want the approved commit", pushes, err)
	}

	// A branch that did not move says nothing about it.
	if _, err := g.pushBranch(map[string]any{"branch": session, "remote_branch": "review/still"}); err != nil {
		t.Fatal(err)
	}
	g.answer(g.pending(1)[0].ID, "approve", "")
	turn := g.turnWith("[branch request ", "· pushed] ")
	if strings.Contains(turn, "moved") {
		t.Errorf("turn %q claims the branch moved", turn)
	}
}
