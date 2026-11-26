//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// Review — the critic the harness launches after a green gate run (ADR 0011,
// Glossary "Critic"): a review-mode session, read-only on the author's
// worktree, on the critic model, with a fresh context.

// criticWorld is a gated author session with a commit on its branch. The
// sandbox is a recording driver: seatbelt cannot nest in the test's own, and a
// read-only session needs the sandbox on.
func criticWorld(t *testing.T) (*shellE2E, *domain.Session) {
	t.Helper()
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	e.orch.Sandbox = &orchestration.Sandbox{Driver: &portstest.RecordingSandboxDriver{}}
	e.commitOnBranch(sess, "feature.txt", "a new line\n")
	return e, sess
}

func (e *shellE2E) commitOnBranch(sess *domain.Session, file, text string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(sess.WorkingDir, file), []byte(text), 0o644); err != nil {
		e.t.Fatal(err)
	}
	gitE2E(e.t, sess.WorkingDir, "add", "-A")
	gitE2E(e.t, sess.WorkingDir, "commit", "-q", "-m", "work "+file)
}

func (e *shellE2E) saveCritic(patch map[string]string) {
	e.t.Helper()
	raw := map[string]json.RawMessage{}
	for k, v := range patch {
		raw[k] = json.RawMessage(v)
	}
	if _, err := e.agentSystem.SaveProject(e.ctx, e.project, raw); err != nil {
		e.t.Fatal(err)
	}
}

// criticsOf are the critic sessions launched for the author.
func (e *shellE2E) criticsOf(author *domain.Session) []*domain.Session {
	var out []*domain.Session
	for _, s := range e.sessions.List(e.ctx, ports.SessionFilter{TicketID: author.TicketID}) {
		if s.CriticOfSessionID == author.ID {
			out = append(out, s)
		}
	}
	return out
}

// waitForCritics waits for n critics of the author, and returns them.
func (e *shellE2E) waitForCritics(author *domain.Session, n int) []*domain.Session {
	e.t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if got := e.criticsOf(author); len(got) >= n {
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
	e.t.Fatalf("%d critics of the author, want %d", len(e.criticsOf(author)), n)
	return nil
}

// noMoreCritics gives a launch the time it would need and checks it did not
// happen.
func (e *shellE2E) noMoreCritics(author *domain.Session, n int) {
	e.t.Helper()
	time.Sleep(400 * time.Millisecond)
	if got := e.criticsOf(author); len(got) != n {
		e.t.Fatalf("%d critics of the author, want %d", len(got), n)
	}
}

func (e *shellE2E) openingOf(sessionID string) string {
	e.t.Helper()
	v, err := e.orch.SessionContext(e.ctx, sessionID)
	if err != nil || len(v.Entries) == 0 {
		e.t.Fatalf("session context: %v %+v", err, v)
	}
	entry := v.Entries[0]
	for _, p := range entry.Instructions {
		if p.ID == domain.PieceOpeningMessage || p.Kind == domain.PieceOpeningMessage {
			text, err := e.orch.SessionContextPiece(e.ctx, sessionID, entry.ID, p.ID)
			if err != nil {
				e.t.Fatal(err)
			}
			return text
		}
	}
	e.t.Fatalf("no opening message among %+v", entry.Instructions)
	return ""
}

func TestAGreenDeclarationRunLaunchesOneReadOnlyCriticOnTheCriticModel(t *testing.T) {
	e, author := criticWorld(t)
	e.saveCritic(map[string]string{"review.critic.model": `"opus"`})

	if out, err := e.callTool(author.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Fatalf("declare_done: %v %v", out, err)
	}
	critics := e.waitForCritics(author, 1)
	c := critics[0]
	runs := e.listedGateRuns(author.ID)
	if c.Mode != domain.SessionModeReview || c.Access != domain.AccessReadOnly || !c.Joined || c.WorkspaceID != author.WorkspaceID {
		t.Errorf("critic = mode %q access %q joined %v workspace %q (author's %q)", c.Mode, c.Access, c.Joined, c.WorkspaceID, author.WorkspaceID)
	}
	if c.Model != "opus" {
		t.Errorf("critic model = %q, want the project's critic.model opus", c.Model)
	}
	if c.CriticOfSessionID != author.ID || len(runs) != 1 || c.CriticGateRunID != runs[0].ID {
		t.Errorf("critic links author %q run %q, runs %+v", c.CriticOfSessionID, c.CriticGateRunID, runs)
	}
	if c.ParentSessionID != "" {
		t.Errorf("a harness-launched critic has no parent session, got %q", c.ParentSessionID)
	}

	opening := e.openingOf(c.ID)
	for _, want := range []string{"You are the critic", "feature.txt", "+a new line"} {
		if !strings.Contains(opening, want) {
			t.Errorf("the critic's starting context lacks %q", want)
		}
	}

	// The same diff green again launches none; a new head launches another.
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	e.noMoreCritics(author, 1)
	e.commitOnBranch(author, "more.txt", "more\n")
	if code, run := e.runGates(author.ID); code != 200 || run.Trigger != domain.GateTriggerPerson {
		t.Fatalf("the person's run: %d %+v", code, run)
	}
	e.waitForCritics(author, 2)
}

func TestNoCriticAfterAPracticeRunOrWhenTheCriticIsOff(t *testing.T) {
	e, author := criticWorld(t)
	if _, err := e.callTool(author.ID, "run_gates", nil); err != nil {
		t.Fatal(err)
	}
	e.noMoreCritics(author, 0)

	e.saveCritic(map[string]string{"review.critic.enabled": `false`})
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	e.noMoreCritics(author, 0)
}
