//go:build !windows

package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
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

func (g *branchGate) pendingApprovals() int {
	g.t.Helper()
	return g.sessions.Get(g.id).PendingApprovals
}

// The count stays what the person can answer, however many times the server
// restarts, and an answer filed before a restart releases the session (ADR 0078).
func TestAPendingRequestReadsOneAfterEveryRestartAndItsAnswerReleasesTheSession(t *testing.T) {
	g := startBranchGateWith(t, "gate.db")
	_, gone := g.askHookCancelable("git branch to-make", "")
	req := g.pending(1)[0]
	gone()
	if got := g.pendingApprovals(); got != 1 {
		t.Fatalf("before the restarts: %d pending, want 1", got)
	}
	for restart := 1; restart <= 2; restart++ {
		g.restartGate()
		if got, status := g.pendingApprovals(), g.status(); got != 1 || status != domain.SessionWaitingApproval {
			t.Fatalf("after restart %d: %d pending, %s; want 1, waiting_approval", restart, got, status)
		}
	}
	if code, body := g.answer(req.ID, "deny", ""); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	if got, status := g.pendingApprovals(), g.status(); got != 0 || status != domain.SessionRunning {
		t.Errorf("after the answer: %d pending, %s; want 0, running", got, status)
	}
}

// A session the server ran, stored as waiting for approval, has no process
// after a restart: it is stopped, and its request expires instead of being
// filed again.
func TestAServerSessionWaitingForApprovalIsStoppedAtBootAndItsRequestExpires(t *testing.T) {
	g := startBranchGateWith(t, "gate.db")
	_, gone := g.askHookCancelable("git branch to-make", "")
	g.pending(1)
	gone()
	g.runsOnServerAfterRestart()

	if got, status := g.pendingApprovals(), g.status(); status != domain.SessionStopped || got != 0 {
		t.Errorf("after the restart: %d pending, %s; want 0, stopped", got, status)
	}
	if got := g.requests("pending"); len(got) != 0 {
		t.Errorf("pending after the restart = %+v, want none", got)
	}
	if got := g.requests("expired"); len(got) != 1 {
		t.Errorf("expired after the restart = %+v, want the one request", got)
	}
}

// A branch action asked while the server is down is refused, and the command
// returns: the gate fails closed (ADR 0069).
func TestABranchActionAskedWhileTheServerIsDownIsRefusedAndDoesNotHang(t *testing.T) {
	g := startBranchGate(t)
	before := g.state()
	g.srv.Close()
	done := make(chan ran, 1)
	go func() { done <- g.git("git checkout -b feature-x") }()
	select {
	case r := <-done:
		if r.code == 0 {
			t.Errorf("the branch action went through: %+v", r)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the command hung with the server down")
	}
	if after := g.state(); after != before {
		t.Errorf("the repository changed:\n%s\nto\n%s", before, after)
	}
}

// runsOnServerAfterRestart restarts the server over the same data with the
// session recorded as one the server ran, as 5fea81b2 was.
func (g *branchGate) runsOnServerAfterRestart() {
	g.t.Helper()
	if err := g.app.Stop(g.ctx); err != nil {
		g.t.Fatal(err)
	}
	g.srv.Close()
	db, err := sqlite.Open(g.cfg.DBPath)
	if err != nil {
		g.t.Fatal(err)
	}
	if err := db.Exec("UPDATE sessions SET runs_on = ? WHERE id = ?", string(domain.RunnerServer), g.id).Error; err != nil {
		g.t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	g.boot()
	g.serve()
}
