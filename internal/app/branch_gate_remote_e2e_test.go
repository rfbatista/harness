//go:build !windows

package app

import (
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// A change to where a push goes waits for the person (contract "Harness server
// ↔ Agent sessions — branch gate" §1 remote rows; ADR 0071).

func (g *branchGate) remoteURL(name string) string {
	return strings.TrimSpace(sh(g.t, g.worktree, nil, "", "git config --get remote."+name+".url").stdout)
}

func TestEveryRemoteChangeIsHeldApprovedAndThenRuns(t *testing.T) {
	g := startBranchGate(t)
	a := g.withRemote("origin")
	b := t.TempDir()
	for _, c := range []struct {
		line   string
		action domain.BranchActionKind
		remote string
		url    string
		check  func() bool
	}{
		{"git remote add review " + b, domain.BranchRemoteAdd, "review", b, func() bool { return g.remoteURL("review") == b }},
		{"git remote set-url review " + a, domain.BranchRemoteSetURL, "review", a, func() bool { return g.remoteURL("review") == a }},
		{"git config remote.review.url " + b, domain.BranchRemoteSetURL, "review", b, func() bool { return g.remoteURL("review") == b }},
		{"git remote rename review reviewed", domain.BranchRemoteRename, "review", "", func() bool { return g.remoteURL("reviewed") == b }},
		{"git remote remove reviewed", domain.BranchRemoteRemove, "reviewed", "", func() bool { return g.remoteURL("reviewed") == "" }},
	} {
		if r := g.git(c.line); r.code != 1 || !strings.Contains(r.stderr, needed) {
			t.Fatalf("%s without a grant: %+v", c.line, r)
		}
		hook := g.askHook(c.line, "")
		req := g.pending(1)[0]
		if req.Action != c.action || req.Remote == nil || req.Remote.Name != c.remote || req.Remote.URL != c.url {
			t.Fatalf("%s: request = %+v (remote %+v)", c.line, req, req.Remote)
		}
		if c.action == domain.BranchRemoteRename && (req.Remote.NewName == nil || *req.Remote.NewName != "reviewed") {
			t.Errorf("rename request carries no new name: %+v", req.Remote)
		}
		if code, body := g.answer(req.ID, "approve", ""); code != http.StatusOK {
			t.Fatalf("answer: %d %s", code, body)
		}
		if a := answered(t, hook); a.decision() != "allow" {
			t.Fatalf("%s: hook answer = %+v", c.line, a)
		}
		if r := g.git(c.line); r.code != 0 || !c.check() {
			t.Fatalf("%s approved: %+v", c.line, r)
		}
	}
}

func TestAnApprovalForOneRemoteUrlDoesNotAllowAnother(t *testing.T) {
	g := startBranchGate(t)
	one, two := t.TempDir(), t.TempDir()
	hook := g.askHook("git remote add review "+one, "")
	req := g.pending(1)[0]
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	if r := g.git("git remote add review " + two); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("another URL with only the first approved: %+v", r)
	}
	if g.remoteURL("review") != "" {
		t.Error("the remote was added with another URL")
	}
}

func TestADeniedRemoteChangeLeavesTheRepositoryAsItWas(t *testing.T) {
	g := startBranchGate(t)
	origin := g.withRemote("origin")
	hook := g.askHook("git remote set-url origin /elsewhere", "")
	req := g.pending(1)[0]
	g.answer(req.ID, "deny", "no")
	if a := answered(t, hook); a.decision() != "deny" || !strings.Contains(a.body, "no") {
		t.Errorf("hook answer = %+v", a)
	}
	if r := g.git("git remote set-url origin /elsewhere"); r.code != 1 || g.remoteURL("origin") != origin {
		t.Errorf("a denied change ran: %+v, origin = %s", r, g.remoteURL("origin"))
	}
}

func TestRemoteReadsAreNeverGatedAndACredentialInAUrlIsRedacted(t *testing.T) {
	g := startBranchGate(t)
	g.withRemote("origin")
	for _, line := range []string{"git remote -v", "git remote show origin", "git remote get-url origin", "git config --get remote.origin.url", "git config remote.origin.url", "git remote"} {
		if r := g.git(line); r.code != 0 {
			t.Errorf("%s was gated: %+v", line, r)
		}
		if a := answered(t, g.askHook(line, "")); a.body != "" {
			t.Errorf("%s: the hook answered %+v", line, a)
		}
	}
	const withSecret = "https://user:s3cret@example.com/x.git"
	hook := g.askHook("git remote add review "+withSecret, "")
	req := g.pending(1)[0]
	if req.Remote == nil || req.Remote.URL != "https://example.com/x.git" || strings.Contains(req.Command, "s3cret") {
		t.Errorf("request = %+v (remote %+v)", req, req.Remote)
	}
	g.answer(req.ID, "deny", "")
	answered(t, hook)
}
