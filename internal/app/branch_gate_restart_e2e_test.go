//go:build !windows

package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// A branch request outlives the server and the hook that waited on it: the
// person's late answer reaches the session as a turn, and the grant waits for
// the rerun (contract "Harness server ↔ Agent sessions — branch gate" §5).

// restartGate restarts the server over the same data and points the session's
// shims at it.
func (g *branchGate) restartGate() {
	g.t.Helper()
	g.restart()
	g.e2eEnv("HARNESS_BRANCH_GATE_URL", g.srv.URL+"/api/sessions/"+g.id+"/branch_gate")
}

// e2eEnv sets name in the env the session's commands run with.
func (g *branchGate) e2eEnv(name, value string) {
	for i, kv := range g.env {
		if strings.HasPrefix(kv, name+"=") {
			g.env[i] = name + "=" + value
			return
		}
	}
	g.env = append(g.env, name+"="+value)
}

// nextTurn is the turn the session's next Stop hook hands it, "" for none.
func (g *branchGate) nextTurn() string {
	g.t.Helper()
	text, err := g.orch.TurnEnded(g.ctx, g.id, false)
	if err != nil {
		g.t.Fatal(err)
	}
	return text
}

func TestALateApprovalIsATurnThatTellsTheAgentToRunTheCommandAgain(t *testing.T) {
	g := startBranchGateWith(t, "gate.db")
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	_, gone := g.askHookCancelable("git checkout -b feature-x", "")
	req := g.pending(1)[0]
	gone()
	g.restartGate()

	if got := g.requests("pending"); len(got) != 1 || got[0].ID != req.ID {
		t.Fatalf("pending after the restart = %+v", got)
	}
	// The session still shows as waiting for approval, and the answer ends it.
	if got := g.status(); got != domain.SessionWaitingApproval {
		t.Errorf("status after the restart = %s, want waiting_approval", got)
	}
	if code, body := g.answer(req.ID, "approve", "keep it small"); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	if got := g.status(); got != domain.SessionRunning {
		t.Errorf("status after the late answer = %s, want running", got)
	}
	want := fmt.Sprintf("[branch request %s · approved] create feature-x from %s. Note: keep it small. Run the command again: git checkout -b feature-x.", req.ID, session)
	eventually(t, "the approved turn", func() error {
		if got := g.nextTurn(); got != want {
			return fmt.Errorf("turn %q, want %q", got, want)
		}
		return nil
	})

	// The grant survived the restart and matches the command in the turn.
	if r := g.git("git checkout -b feature-x"); r.code != 0 {
		t.Fatalf("the rerun: %+v", r)
	}
	g.setup("checkout " + session)
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("the grant was used twice: %+v", r)
	}
}

func TestALateDenialIsATurnWithTheNoteAndNoGrant(t *testing.T) {
	g := startBranchGateWith(t, "gate.db")
	_, gone := g.askHookCancelable("git branch -D to-delete", "")
	req := g.pending(1)[0]
	gone()
	g.restartGate()
	g.answer(req.ID, "deny", "keep that one")
	want := fmt.Sprintf("[branch request %s · denied] delete to-delete. Note: keep that one.", req.ID)
	eventually(t, "the denied turn", func() error {
		if got := g.nextTurn(); got != want {
			return fmt.Errorf("turn %q, want %q", got, want)
		}
		return nil
	})
	if r := g.git("git branch -D to-delete"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("a denied delete ran: %+v", r)
	}
}

func TestAnAnswerTheHookWaitedOnSendsNoTurn(t *testing.T) {
	g := startBranchGate(t)
	hook := g.askHook("git branch z", "")
	req := g.pending(1)[0]
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	if got := g.nextTurn(); got != "" {
		t.Errorf("turn %q, want none", got)
	}
}

func TestRequestsLeftOpenWhenTheSessionEndsExpire(t *testing.T) {
	g := startBranchGate(t)
	// One approved and never used, one still pending.
	approved := g.askHook("git branch a", "")
	byBranch := map[string]domain.BranchRequest{}
	pendingHook, gone := g.askHookCancelable("git branch b", "")
	_ = pendingHook
	for _, r := range g.pending(2) {
		byBranch[r.Branch] = r
	}
	g.answer(byBranch["a"].ID, "approve", "")
	answered(t, approved)
	gone()
	if _, err := g.orch.EndInteractive(g.ctx, g.id, 0, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the requests expired", func() error {
		for _, r := range g.requests("") {
			if r.State != domain.BranchRequestExpired {
				return fmt.Errorf("%s (%s) is %s", r.ID, r.Branch, r.State)
			}
		}
		return nil
	})
}
