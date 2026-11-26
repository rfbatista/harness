//go:build !windows

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// Review — the edges of the critic's launch and report that the happy-path
// e2e leaves: races, retries, a changed model, a missing architect, an author
// that ended, the read-only policy and the visual QA brief from two starters.

func TestTwoGreenRunsFinishingTogetherOnOneDiffLaunchOneCritic(t *testing.T) {
	e, author := criticWorld(t)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.runGates(author.ID)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = e.callTool(author.ID, "declare_done", nil)
	}()
	wg.Wait()

	e.waitForCritics(author, 1)
	e.noMoreCritics(author, 1)
}

func TestAGreenRunThePersonTriggeredLaunchesTheCriticLinkedToThatRun(t *testing.T) {
	e, author := criticWorld(t)

	code, run := e.runGates(author.ID)
	if code != http.StatusOK || run.Trigger != domain.GateTriggerPerson || run.Result != domain.GateRunGreen {
		t.Fatalf("the person's run: %d %+v", code, run)
	}
	c := e.waitForCritics(author, 1)[0]
	if c.CriticGateRunID != run.ID || c.CriticOfSessionID != author.ID {
		t.Errorf("critic links run %q author %q, want %q and %q", c.CriticGateRunID, c.CriticOfSessionID, run.ID, author.ID)
	}
}

func TestARedDeclarationLaunchesNoCriticAndTheGreenOneAfterTheFixLaunchesOne(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "test -f fixed"
	author := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})
	e.orch.Sandbox = &orchestration.Sandbox{Driver: &portstest.RecordingSandboxDriver{}}
	e.commitOnBranch(author, "feature.txt", "a new line\n")

	if _, err := e.callTool(author.ID, "declare_done", nil); err == nil {
		t.Fatal("a red gate accepted the declaration")
	}
	e.noMoreCritics(author, 0)

	if err := os.WriteFile(filepath.Join(author.WorkingDir, "fixed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := e.callTool(author.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Fatalf("declare_done after the fix: %v %v", out, err)
	}
	e.waitForCritics(author, 1)
	e.noMoreCritics(author, 1)
}

func TestAChangeOfTheCriticModelBetweenRunsShowsOnTheNextCritic(t *testing.T) {
	e, author := criticWorld(t)
	e.saveCritic(map[string]string{"review.critic.model": `"opus"`})
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	e.waitForCritics(author, 1)

	e.saveCritic(map[string]string{"review.critic.model": `"sonnet"`})
	e.commitOnBranch(author, "more.txt", "more\n")
	if code, _ := e.runGates(author.ID); code != http.StatusOK {
		t.Fatalf("run gates: %d", code)
	}
	models := map[string]int{}
	for _, c := range e.waitForCritics(author, 2) {
		models[c.Model]++
	}
	if models["opus"] != 1 || models["sonnet"] != 1 {
		t.Errorf("critic models = %v, want one opus (before) and one sonnet (after the change)", models)
	}
}

func TestAnAuthorWithNoArchitectStillGetsTheCriticsFindingsAndTheReportSucceeds(t *testing.T) {
	e, author := criticWorld(t)
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	critic := e.waitForCritics(author, 1)[0]

	findings := "Verdict: approved\nfeature.txt:1 · minor · name it"
	if _, err := e.callTool(critic.ID, "message_architect", map[string]any{"kind": "status_report", "status": "done", "body": findings}); err != nil {
		t.Fatalf("the critic's report on a task with no architect: %v", err)
	}
	if turn := e.stopHook(author.ID); !strings.Contains(turn, "Critic findings") || !strings.Contains(turn, "name it") {
		t.Errorf("the author's next turn = %q", turn)
	}
}

func TestFindingsForAnAuthorThatEndedStillReachTheArchitectAndTheReportDoesNotFail(t *testing.T) {
	e, author := criticWorld(t)
	arch, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: author.TicketID, Mode: "architect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	critic := e.waitForCritics(author, 1)[0]
	if _, err := e.orch.EndInteractive(e.ctx, author.ID, 0, true); err != nil {
		t.Fatal(err)
	}

	findings := "Verdict: changes_requested\nfeature.txt:1 · major · late finding"
	if _, err := e.callTool(critic.ID, "message_architect", map[string]any{"kind": "status_report", "status": "done", "body": findings}); err != nil {
		t.Fatalf("the critic's report after the author ended: %v", err)
	}
	msgs, err := e.channel.ListTaskMessages(e.ctx, author.TicketID, ports.TaskMessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var toArchitect int
	for _, m := range msgs {
		if m.FromSessionID == critic.ID && m.ToSessionID == arch.ID && strings.Contains(m.Body, "late finding") {
			toArchitect++
		}
	}
	if toArchitect != 1 {
		t.Errorf("%d findings messages to the architect, want 1 (%d messages)", toArchitect, len(msgs))
	}
}

func TestTheCriticsSandboxLeavesTheAuthorsWorktreeAndItsGitDirNotWritable(t *testing.T) {
	e, author := criticWorld(t)
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	critic := e.waitForCritics(author, 1)[0]
	gitDir := strings.TrimSpace(gitE2E(t, author.WorkingDir, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	wt, _ := filepath.EvalSymlinks(author.WorkingDir)

	drv := e.orch.Sandbox.Driver.(*portstest.RecordingSandboxDriver)
	calls := drv.Calls()
	if len(calls) == 0 {
		t.Fatalf("no sandbox policy recorded for the critic %s", critic.ID)
	}
	// The critic's start is the last wrap; the author's own (writable) came first.
	call := calls[len(calls)-1]
	if call.Policy.WorkingDir != author.WorkingDir && call.Policy.WorkingDir != wt {
		t.Fatalf("the last policy is for %q, not the author's worktree", call.Policy.WorkingDir)
	}
	// Tool writes and shell writes (a commit included) run inside this one
	// confinement, so one policy covers both.
	for _, w := range call.Policy.Writable {
		rw, _ := filepath.EvalSymlinks(w)
		for _, guarded := range []string{author.WorkingDir, wt, gitDir} {
			if w == guarded || rw == guarded || strings.HasPrefix(w, guarded+string(filepath.Separator)) {
				t.Errorf("the critic's sandbox lets it write %q, inside the guarded %q", w, guarded)
			}
		}
	}
}

func TestAQASessionTheEndpointStartsAndOneAnArchitectStartsGetTheSameDeclarationBrief(t *testing.T) {
	e, author := criticWorld(t)
	e.orch.AgentSystem = e.agentSystem
	e.declareUI()

	code, body := e.startVisualQA(author.ID)
	if code != http.StatusCreated {
		t.Fatalf("start = %d %v", code, body)
	}
	viaEndpoint := e.openingOf(body["id"].(string))
	qa, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: author.TicketID, Mode: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	viaArchitect := e.openingOf(qa.ID)

	ui := declaredUIBrief(t)
	if !strings.Contains(viaEndpoint, ui) || !strings.Contains(viaArchitect, ui) {
		t.Errorf("the declaration brief is not in both openings:\nendpoint:\n%s\narchitect:\n%s", viaEndpoint, viaArchitect)
	}
}

// declaredUIBrief is the brief text of the declaration the tests declare.
func declaredUIBrief(t *testing.T) string {
	t.Helper()
	return domain.VisualQABrief(domain.VisualQAUI{
		StartCommand: "npm run dev", URL: "http://localhost:5173",
		Routes: []string{"/", "/login"}, Viewports: []string{"1280x800", "390x844"}, UIPaths: []string{"web/**"},
	})
}

func TestVisualQAOnASessionThatIsNotGatedFollowsTheRepositoryDeclaration(t *testing.T) {
	e := newShellE2E(t)
	sess, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Plain"), Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := e.startVisualQA(sess.ID); code != http.StatusConflict || body["code"] != "VISUAL_QA_NOT_DECLARED" {
		t.Fatalf("undeclared, not gated: %d %v", code, body)
	}
}
