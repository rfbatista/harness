//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — a gate run on the person's request (ADR 0012, 0014): the harness
// runs the session's gates in its worktree and records what each did.

// greenGates is a command for each required gate that passes.
func greenGates() map[domain.GateKind]string {
	g := map[domain.GateKind]string{}
	for _, k := range domain.RequiredGateKinds {
		g[k] = "true"
	}
	return g
}

// gatedSession starts an implement session whose guard configuration is the
// fake's.
func (e *shellE2E) gatedSession(settings *fakeAgentSystem) *domain.Session {
	e.t.Helper()
	e.orch.AgentSystem = settings
	// The gates run unconfined here: a sandbox cannot nest in the test's own.
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		e.t.Fatal(err)
	}
	sess, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Gated"), Mode: "implement"})
	if err != nil {
		e.t.Fatal(err)
	}
	return sess
}

func (e *shellE2E) runGates(sessionID string) (int, domain.GateRun) {
	e.t.Helper()
	res, err := http.Post(e.srv.URL+"/api/sessions/"+sessionID+"/gate-runs", "application/json", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var run domain.GateRun
	_ = json.NewDecoder(res.Body).Decode(&run)
	return res.StatusCode, run
}

func (e *shellE2E) listedGateRuns(sessionID string) []domain.GateRun {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/sessions/" + sessionID + "/gate-runs")
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Runs []domain.GateRun `json:"runs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		e.t.Fatal(err)
	}
	return body.Runs
}

func TestThePersonsGateRunRecordsEveryGateAndIsGreenWhenAllExitZero(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})

	code, run := e.runGates(sess.ID)
	if code != http.StatusOK || run.Result != domain.GateRunGreen || run.Trigger != domain.GateTriggerPerson || run.Counted {
		t.Fatalf("run = %d %+v", code, run)
	}
	if len(run.Gates) != 5 || run.Gates[0].Kind != domain.GateBuild || run.Gates[0].Command != "true" || run.Gates[0].ExitCode != 0 {
		t.Errorf("gates = %+v", run.Gates)
	}
	if runs := e.listedGateRuns(sess.ID); len(runs) != 1 || runs[0].ID != run.ID {
		t.Errorf("listed runs = %+v", runs)
	}
}

func TestAGateThatExitsNonZeroMakesTheRunRedAndKeepsItsOutputAndDuration(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateTest] = "echo boom; echo failing >&2; exit 3"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})

	_, run := e.runGates(sess.ID)
	if run.Result != domain.GateRunRed || len(run.Gates) != 5 {
		t.Fatalf("run = %+v", run)
	}
	var test domain.GateResult
	for _, g := range run.Gates {
		if g.Kind == domain.GateTest {
			test = g
		}
	}
	if test.ExitCode != 3 || !strings.Contains(test.Output, "boom") || !strings.Contains(test.Output, "failing") || test.DurationMs < 0 {
		t.Errorf("test gate = %+v", test)
	}
}

func TestAGateOverItsTimeoutIsKilledAndRecordedRed(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateLint] = "sleep 30"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates, timeouts: map[domain.GateKind]int{domain.GateLint: 1}})

	_, run := e.runGates(sess.ID)
	var lint domain.GateResult
	for _, g := range run.Gates {
		if g.Kind == domain.GateLint {
			lint = g
		}
	}
	if run.Result != domain.GateRunRed || !lint.TimedOut || lint.DurationMs > 10_000 || !strings.Contains(lint.Output, "timed out") {
		t.Errorf("run %q lint = %+v", run.Result, lint)
	}
}

func TestARepositoryMissingARequiredGateEndsNotPassedNamingTheKindsAndRunsNothing(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	delete(gates, domain.GateSecretScan)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})

	code, run := e.runGates(sess.ID)
	if code != http.StatusOK || run.Result != domain.GateRunNotPassed || len(run.MissingKinds) != 1 || run.MissingKinds[0] != domain.GateSecretScan || len(run.Gates) != 0 {
		t.Errorf("run = %d %+v", code, run)
	}
}

func TestAGateRunExecutesInTheSessionsWorktreeAndAnEndedSessionCanBeRun(t *testing.T) {
	e := newShellE2E(t)
	gates := greenGates()
	gates[domain.GateBuild] = "pwd -P"
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: gates})
	e.orch.Transcripts = transcriptsThatResume{}
	if _, err := e.orch.EndInteractive(e.ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}

	_, run := e.runGates(sess.ID)
	want, _ := filepath.EvalSymlinks(sess.WorkingDir)
	if got := strings.TrimSpace(run.Gates[0].Output); got != want {
		t.Errorf("the gate ran in %q, want the worktree %q", got, want)
	}
}

func TestAGateRunWritesNothingBeyondWhatTheGateCommandDoes(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	before := listing(t, sess.WorkingDir)

	e.runGates(sess.ID)
	if after := listing(t, sess.WorkingDir); after != before {
		t.Errorf("the worktree changed: %q -> %q", before, after)
	}
}

func TestAGateRunOfASessionThatIsNotGatedIsRefusedAndAnUnknownSessionIs404(t *testing.T) {
	e := newShellE2E(t)
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	plain, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Mode-less")})
	if err != nil {
		t.Fatal(err)
	}

	if code, _ := e.runGates(plain.ID); code != http.StatusConflict {
		t.Errorf("a mode-less session with no diff: status %d, want 409", code)
	}
	if code, _ := e.runGates("nope"); code != http.StatusNotFound {
		t.Errorf("an unknown session: status %d, want 404", code)
	}
}

// listing names every file under dir, but git's own directory.
func listing(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if err == nil {
			names = append(names, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(names, "\n")
}

func TestAGateRunPublishesOneStartedAndOneFinishedEventOnTheProjectFeed(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	feed, err := e.orch.FollowProject(e.ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}

	_, run := e.runGates(sess.ID)

	var got []ports.GateRunChange
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case ch := <-feed:
			if ch.GateRun != nil {
				got = append(got, *ch.GateRun)
			}
		case <-deadline:
			t.Fatalf("events so far: %+v", got)
		}
	}
	start, end := got[0], got[1]
	if start.Phase != "started" || start.SessionID != sess.ID || start.RunID != run.ID || start.Trigger != domain.GateTriggerPerson || start.ProjectID != e.project {
		t.Errorf("started = %+v", start)
	}
	if end.Phase != "finished" || end.RunID != run.ID || end.Result != domain.GateRunGreen || end.Counted {
		t.Errorf("finished = %+v", end)
	}
}
