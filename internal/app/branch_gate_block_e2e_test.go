//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The branch block (ADR 0088; contract "Harness server ↔ Agent sessions —
// branch gate" §1a): while it is on for a session's mode, creating, switching
// and deleting branches is refused at once by the hook and by the git shim,
// with no branch request filed.

// setBlock writes a mode's branch block through the settings endpoint, as the
// Server page does.
func (g *branchGate) setBlock(mode domain.SessionMode, value string) (int, string) {
	g.t.Helper()
	b, _ := json.Marshal(map[string]any{"settings": map[string]string{domain.BranchBlockKey(mode): value}})
	res, err := http.Post(g.srv.URL+"/api/update_settings", "application/json", bytes.NewReader(b))
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.String()
}

// blockedMessage is the contract's message for the session.
func (g *branchGate) blockedMessage(modeName string) string {
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	return "BRANCH_ACTION_BLOCKED: creating, switching and deleting branches is blocked for " + modeName +
		" sessions. Stay on your session branch " + session + ". Pushing still goes through a branch request."
}

var (
	blockedCreates = []string{
		"git checkout -b feature-x", "git checkout -B feature-x", "git switch -c feature-x", "git switch -C feature-x",
		"git branch feature-x", "git branch feature-x other", "git checkout -b feature-x other",
		"git worktree add -b feature-x ../wt-x", "git stash branch feature-x",
	}
	blockedSwitches = []string{
		"git checkout other", "git switch other", "git checkout --detach other", "git switch --detach other", "git checkout $(git rev-parse HEAD)",
		"git worktree add ../wt-other other", "git rebase main other",
	}
	blockedDeletes = []string{"git branch -d to-delete", "git branch -D to-delete", "git branch -D to-delete other"}
)

func TestABlockedModeRefusesEveryCreateSwitchAndDeleteFormAtOnceInTheShim(t *testing.T) {
	g := startBlockedBranchGate(t)
	want := g.blockedMessage("Implement")
	var all []string
	all = append(append(append(all, blockedCreates...), blockedSwitches...), blockedDeletes...)
	for _, line := range all {
		before := g.state()
		r := g.git(line)
		if r.code != 1 || !strings.Contains(r.stderr, want) {
			t.Errorf("%s: %+v, want exit 1 with %s", line, r, want)
		}
		if after := g.state(); after != before {
			t.Errorf("%s changed the repository:\n%s\nto\n%s", line, before, after)
		}
	}
	if got := g.requests(""); len(got) != 0 {
		t.Errorf("a blocked action filed requests: %+v", got)
	}
}

func TestABlockedModeDeniesEveryCreateSwitchAndDeleteFormAtOnceInTheHook(t *testing.T) {
	g := startBlockedBranchGate(t)
	want := g.blockedMessage("Implement")
	var all []string
	all = append(append(append(all, blockedCreates...), blockedSwitches[:2]...), blockedDeletes...)
	all = append(all, "git checkout --detach other", "git worktree add ../wt-other other")
	for _, line := range all {
		a := answered(t, g.askHook(line, ""))
		var out struct {
			Hook struct {
				Reason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		_ = json.Unmarshal([]byte(a.body), &out)
		if a.decision() != "deny" || out.Hook.Reason != want {
			t.Errorf("%s: %+v, want deny with %q", line, a, want)
		}
	}
	if got := g.requests(""); len(got) != 0 {
		t.Errorf("a blocked action filed requests: %+v", got)
	}
}

func TestASwitchBackToTheSessionBranchAndTheNeverGatedFormsPassWhileBlocked(t *testing.T) {
	g := startBlockedBranchGate(t)
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	for _, line := range []string{"git checkout " + session, "git switch " + session, "git status", "git branch --list", "git log --oneline"} {
		if r := g.git(line); r.code != 0 || strings.Contains(r.stderr, "BRANCH_") {
			t.Errorf("%s: %+v", line, r)
		}
		if a := answered(t, g.askHook(line, "")); a.body != "" {
			t.Errorf("%s: the hook answered %+v", line, a)
		}
	}
}

func TestRenameAndPushStillFileRequestsWhileBlocked(t *testing.T) {
	g := startBlockedBranchGate(t)
	if r := g.git("git branch -m to-rename renamed"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("rename in the shim: %+v", r)
	}
	if r := g.git("git push origin other"); strings.Contains(r.stderr, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("push in the shim: %+v", r)
	}
	hook, cancel := g.askHookCancelable("git branch -m to-rename renamed", "")
	defer cancel()
	req := g.pending(1)[0]
	waits(t, hook)
	if req.Action != domain.BranchRename {
		t.Errorf("request = %+v", req)
	}
}

func TestAForcePushStaysRefusedWithItsOwnReasonWhileBlocked(t *testing.T) {
	g := startBlockedBranchGate(t)
	r := g.git("git push --force origin other")
	if r.code != 1 || r.stderr == "" || strings.Contains(r.stderr, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("force push: %+v", r)
	}
	a := answered(t, g.askHook("git push --force origin other", ""))
	if a.decision() != "deny" || strings.Contains(a.body, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("force push in the hook: %+v", a)
	}
}

func TestACommandWithABlockedActionIsBlockedWholeAndFilesNoRequest(t *testing.T) {
	g := startBlockedBranchGate(t)
	a := answered(t, g.askHook("git branch -m to-rename renamed && git checkout -b feature-x", ""))
	if a.decision() != "deny" || !strings.Contains(a.body, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("hook: %+v", a)
	}
	a = answered(t, g.askHook("git checkout -b feature-x && git branch -m to-rename renamed", ""))
	if a.decision() != "deny" || !strings.Contains(a.body, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("hook, other order: %+v", a)
	}
	if got := g.requests(""); len(got) != 0 {
		t.Errorf("requests filed for a blocked command: %+v", got)
	}
}

func TestTurningTheBlockOffAndOnReachesTheRunningSessionAtItsNextAction(t *testing.T) {
	g := startBlockedBranchGate(t)
	want := g.blockedMessage("Implement")
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, want) {
		t.Fatalf("blocked at first: %+v", r)
	}
	if code, body := g.setBlock(domain.SessionModeImplement, "off"); code != http.StatusOK {
		t.Fatalf("set off: %d %s", code, body)
	}
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("off: the shim should ask for a request: %+v", r)
	}
	hook, cancel := g.askHookCancelable("git checkout -b feature-x", "")
	defer cancel()
	g.pending(1)
	waits(t, hook)
	if code, body := g.setBlock(domain.SessionModeImplement, ""); code != http.StatusOK {
		t.Fatalf("clear: %d %s", code, body)
	}
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, want) {
		t.Errorf("cleared (built-in on): %+v", r)
	}
	if strings.Contains(g.state(), "feature-x") {
		t.Error("a branch was made")
	}
}

func TestAGrantLeftFromBeforeTheBlockIsRefusedNotUsed(t *testing.T) {
	g := startBlockedBranchGate(t)
	g.setBlock(domain.SessionModeImplement, "off")
	hook := g.askHook("git checkout -b feature-x", "")
	req := g.pending(1)[0]
	if code, body := g.answer(req.ID, "approve", ""); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	if a := answered(t, hook); a.decision() != "allow" {
		t.Fatalf("hook: %+v", a)
	}
	g.setBlock(domain.SessionModeImplement, "on")
	want := g.blockedMessage("Implement")
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, want) {
		t.Errorf("shim with a stale grant: %+v", r)
	}
	if strings.Contains(g.state(), "feature-x") {
		t.Error("the stale grant made a branch")
	}
	if used := g.requests("used"); len(used) != 0 {
		t.Errorf("the grant was used up: %+v", used)
	}
	// Off again: the approval is still there, and works.
	g.setBlock(domain.SessionModeImplement, "off")
	if r := g.git("git checkout -b feature-x"); r.code != 0 {
		t.Errorf("the grant after turning the block off: %+v", r)
	}
}

func TestEveryModeFollowsItsOwnKey(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeDefault, domain.SessionModeArchitect, domain.SessionModeImplement, domain.SessionModeDesign,
		domain.SessionModeReview, domain.SessionModeTest, domain.SessionModeQA, domain.SessionModeOperate} {
		t.Run(domain.ModeName(mode), func(t *testing.T) {
			g := startBranchGateOn(t, ":memory:", mode, domain.BranchBlockOff)
			// Its own key is off: not blocked. Every other key is built-in on.
			if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, needed) {
				t.Errorf("own key off: %+v", r)
			}
			// The other keys off, its own on again: blocked, naming its mode.
			for _, other := range []domain.SessionMode{domain.SessionModeDefault, domain.SessionModeArchitect, domain.SessionModeImplement, domain.SessionModeDesign, domain.SessionModeProduct, domain.SessionModeDesignSystem,
				domain.SessionModeReview, domain.SessionModeTest, domain.SessionModeQA, domain.SessionModeOperate} {
				if other != mode {
					g.setBlock(other, "off")
				}
			}
			g.setBlock(mode, "on")
			if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, g.blockedMessage(domain.ModeName(mode))) {
				t.Errorf("own key on: %+v", r)
			}
		})
	}
}

func TestABlockWriteWithAnotherValueIsRefusedNamingTheKey(t *testing.T) {
	g := startBlockedBranchGate(t)
	code, body := g.setBlock(domain.SessionModeImplement, "maybe")
	if code != http.StatusBadRequest || !strings.Contains(body, "INVALID_INPUT") || !strings.Contains(body, "branches.block.mode.implement") ||
		!strings.Contains(body, "on") || !strings.Contains(body, "off") {
		t.Errorf("write maybe: %d %s", code, body)
	}
	if r := g.git("git checkout -b feature-x"); !strings.Contains(r.stderr, "BRANCH_ACTION_BLOCKED") {
		t.Errorf("the refused write changed the block: %+v", r)
	}
}

func TestAnUnreachableHarnessStillFailsClosedForABlockedAction(t *testing.T) {
	g := startBlockedBranchGate(t)
	down := append(append([]string{}, g.env...), "HARNESS_BRANCH_GATE_URL=http://127.0.0.1:1/api/sessions/x/branch_gate")
	r := sh(t, g.worktree, down, "", "git checkout -b feature-x")
	if r.code != 1 || !strings.Contains(r.stderr, "BRANCH_GATE_UNAVAILABLE") {
		t.Errorf("%+v", r)
	}
}
