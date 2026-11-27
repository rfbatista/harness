//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
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
	fake := &fakeAgentSystem{retry: 3, gates: greenGates()}
	sess := e.gatedSession(fake)
	// The done declaration is not about task memory here: Learn's check would
	// refuse the first declaration for an unchanged progress.md.
	e.orch.AgentSystem = noTaskMemoryCheck{fake}
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

	// The critic's sandbox holds the author's worktree read-only: the OS
	// refuses every write and every commit.
	drv := e.orch.Sandbox.Driver.(*portstest.RecordingSandboxDriver)
	calls := drv.Calls()
	if len(calls) == 0 {
		t.Fatal("the critic was started without a sandbox")
	}
	if p := calls[len(calls)-1].Policy; slices.Contains(p.Writable, author.WorkingDir) || !slices.Contains(p.Readable, author.WorkingDir) {
		t.Errorf("the critic's sandbox policy = writable %v, readable %v: want the author's worktree read-only", p.Writable, p.Readable)
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

// stopHook is the session's Stop hook, as the CLI posts it; it returns the
// reason claude is told to go on with, "" when it may stop.
func (e *shellE2E) stopHook(id string) string {
	e.t.Helper()
	res, err := http.Post(e.srv.URL+httpapi.InteractiveSessionHookPath+"?session_id="+id+"&event=Stop", "application/json",
		strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	reason, _ := out["reason"].(string)
	return reason
}

func TestTheCriticsFindingsReachTheAuthorAsATurnAndTheArchitectInTheTaskMessages(t *testing.T) {
	e, author := criticWorld(t)
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: author.TicketID, Mode: "architect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	critic := e.waitForCritics(author, 1)[0]

	findings := "Verdict: changes_requested\nfeature.txt:1 · major · the new line has no test"
	if _, err := e.callTool(critic.ID, "message_architect", map[string]any{"kind": "status_report", "status": "done", "body": findings}); err != nil {
		t.Fatalf("the critic's report: %v", err)
	}

	msgs, err := e.channel.ListTaskMessages(e.ctx, author.TicketID, ports.TaskMessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var toAuthor, toArchitect int
	for _, m := range msgs {
		if m.FromSessionID != critic.ID || !strings.Contains(m.Body, "feature.txt:1 · major") {
			continue
		}
		switch m.ToSessionID {
		case author.ID:
			toAuthor++
		case arch.ID:
			toArchitect++
		}
	}
	if toAuthor != 1 || toArchitect != 1 {
		t.Fatalf("findings messages: %d to the author, %d to the architect, want one each (%d messages)", toAuthor, toArchitect, len(msgs))
	}

	// The author is mid-turn: the findings are among the turns it is handed at
	// its Stop, in whatever order the channel queued them.
	var turns []string
	found := false
	for i := 0; i < 5 && !found; i++ {
		turn := e.stopHook(author.ID)
		turns = append(turns, turn)
		found = strings.Contains(turn, "[task message ") && strings.Contains(turn, "Subject: Critic findings") && strings.Contains(turn, "the new line has no test")
	}
	if !found {
		t.Errorf("the author was never handed the findings: %q", turns)
	}

	// Advisory: a red verdict leaves the author's declaration accepted.
	if out, err := e.callTool(author.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Errorf("declare_done after the findings: %v %v", out, err)
	}
}

func TestWithTheSandboxOffNoCriticStartsAndTheSkipIsRecordedOnTheAuthorAndPublished(t *testing.T) {
	e, author := criticWorld(t)
	drv := e.orch.Sandbox.Driver
	e.orch.Sandbox = &orchestration.Sandbox{Driver: drv, Enabled: func() bool { return false }}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	feed, err := e.orch.FollowProject(ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}

	if out, err := e.callTool(author.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Fatalf("a missing critic never refuses the declaration: %v %v", out, err)
	}
	deadline := time.After(40 * time.Second)
	for published := false; !published; {
		select {
		case c := <-feed:
			published = c.Session != nil && c.Session.ID == author.ID && c.Session.CriticSkip == "critic skipped: sandbox off"
		case <-deadline:
			t.Fatal("the skip was not published on the project feed")
		}
	}
	if got := e.sessions.Get(author.ID); got.CriticSkip != "critic skipped: sandbox off" {
		t.Errorf("recorded on the author = %q", got.CriticSkip)
	}
	e.noMoreCritics(author, 0)

	// With the sandbox back on, the same diff is reviewed and the note goes.
	e.orch.Sandbox = &orchestration.Sandbox{Driver: drv}
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	e.waitForCritics(author, 1)
	for deadline := time.Now().Add(20 * time.Second); e.sessions.Get(author.ID).CriticSkip != "" && time.Now().Before(deadline); {
		time.Sleep(25 * time.Millisecond)
	}
	if got := e.sessions.Get(author.ID).CriticSkip; got != "" {
		t.Errorf("the skip note stayed after a critic ran: %q", got)
	}
}

// noTaskMemoryCheck is the guard fake with Learn's task memory update check
// switched off.
type noTaskMemoryCheck struct{ *fakeAgentSystem }

func (n noTaskMemoryCheck) Effective(ctx context.Context, projectID, repositoryID string) (domain.EffectiveAgentSystem, error) {
	eff, err := n.fakeAgentSystem.Effective(ctx, projectID, repositoryID)
	eff.Learn.TaskMemory.RequireUpdate.Value = false
	return eff, err
}
