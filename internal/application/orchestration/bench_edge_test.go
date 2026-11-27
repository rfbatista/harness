package orchestration

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Seam B edge cases: the scripted fake CLI through the mock tool layer.

func TestAScriptedCallToAToolOfAProjectMCPServerIsAnErrorTheRunRecordsAndPenalises(t *testing.T) {
	// The agent is attached to the project's github server; the run offers the
	// mock server only, so a call to one of its tools reaches nothing.
	w := newRunWorld(t, `[{"tool":"read_file","params":{"path":"README.md"}},{"tool":"mcp__github__create_issue","params":{"title":"x"}}]`, "Read the README.")
	run := w.startRun(t)
	card := w.scorecard(t, run)

	c := card.Cases[0]
	if len(c.Calls) != 2 || c.Calls[1].Tool != "mcp__github__create_issue" {
		t.Fatalf("calls = %+v", c.Calls)
	}
	if got := c.ToolUse.RightTool.Score; got != 0.5 {
		t.Fatalf("right_tool = %v, want 0.5 (one unexpected call over two)", got)
	}
	if !slices.ContainsFunc(c.ToolUse.RightTool.Reasons, func(r string) bool { return strings.Contains(r, "unexpected call 2") }) {
		t.Fatalf("reasons = %v", c.ToolUse.RightTool.Reasons)
	}
	l := w.launch(t)
	if !slices.Equal(l.Servers, []string{"bench"}) || len(l.Answers) != 2 || !strings.Contains(l.Answers[1], "error") {
		t.Fatalf("servers %v answers %v", l.Servers, l.Answers)
	}
}

func TestARunWhoseCLIMakesNoCallScoresZeroOnRightToolAndStillFinishes(t *testing.T) {
	w := newRunWorld(t, `[]`, "Read the README.")
	run := w.startRun(t)
	card := w.scorecard(t, run)
	c := card.Cases[0]
	if run.Status != domain.BenchRunFinished || c.Error != "" || len(c.Calls) != 0 || !c.ToolUse.RightTool.Scored || c.ToolUse.RightTool.Score != 0 {
		t.Fatalf("run %+v case %+v", run, c)
	}
}

func TestAMockToolCannedAsAnErrorAnswersTheCLIWithAnError(t *testing.T) {
	w := newRunWorld(t, readScript, "Read the README.")
	w.cases = nil
	cases, _ := w.bench.ListCases(context.Background(), "p1")
	for _, c := range cases {
		if err := w.bench.DeleteCase(context.Background(), "p1", c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.bench.CreateCase(context.Background(), "p1", domain.BenchCase{
		Prompt: "Read it.",
		Tools:  []domain.BenchTool{{Name: "read_file", Result: "permission denied", IsError: true}},
		Expect: domain.BenchExpectation{Tools: []domain.BenchExpectedCall{{Tool: "read_file"}}},
	}); err != nil {
		t.Fatal(err)
	}
	w.startRun(t)
	answers := w.launch(t).Answers
	if len(answers) != 1 || !strings.Contains(answers[0], "permission denied") || !strings.Contains(answers[0], "rror") {
		t.Fatalf("answers = %v", answers)
	}
}

func TestARunWithAnUnknownAgentIsRefusedAndStartsNoCLI(t *testing.T) {
	w := newRunWorld(t, readScript)
	_, err := w.bench.StartRun(context.Background(), "p1", domain.BenchRunRequest{AgentID: "ghost", Model: "sonnet"})
	if err == nil {
		t.Fatal("a run on an agent that does not exist was started")
	}
	var se *domain.StructuredError
	_ = errors.As(err, &se)
	if _, statErr := statLaunch(w); statErr == nil {
		t.Fatal("a CLI was started for a refused run")
	}
}

func TestACaseDeletedBetweenTwoRunsShowsAsRemovedInTheComparison(t *testing.T) {
	w := newRunWorld(t, readScript)
	first := w.scorecard(t, w.startRun(t))
	if err := w.bench.DeleteCase(context.Background(), "p1", w.cases[1].ID); err != nil {
		t.Fatal(err)
	}
	second := w.scorecard(t, w.startRun(t))
	cmp, err := w.bench.CompareScorecard(context.Background(), "p1", second.ID, "")
	if err != nil || cmp.PreviousID != first.ID || len(second.Cases) != 1 {
		t.Fatalf("cmp %+v err %v", cmp, err)
	}
	var removed int
	for _, c := range cmp.Cases {
		if c.Status == domain.BenchCaseRemoved && c.CaseID == w.cases[1].ID {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("cases = %+v", cmp.Cases)
	}
}

func TestASecondStartWhileACLIIsRunningIsRefusedAndTheFirstRunSurvives(t *testing.T) {
	w := newRunWorld(t, readScript, "HANG on this one")
	run, err := w.bench.StartRun(context.Background(), "p1", domain.BenchRunRequest{AgentID: "dev", RepositoryID: "r1", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.bench.StartRun(context.Background(), "p1", domain.BenchRunRequest{AgentID: "dev", RepositoryID: "r1", Model: "sonnet"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "BENCH_RUN_IN_PROGRESS" {
		t.Fatalf("second start = %v", err)
	}
	if got, _ := w.bench.GetRun(context.Background(), "p1", run.ID); got.Status != domain.BenchRunRunning {
		t.Fatalf("first run = %+v", got)
	}
	if _, err := w.bench.CancelRun(context.Background(), "p1", run.ID); err != nil {
		t.Fatal(err)
	}
	if ended := w.waitEnd(t, run.ID); ended.Status != domain.BenchRunCancelled {
		t.Fatalf("ended = %+v", ended)
	}
}

func statLaunch(w *runWorld) (os.FileInfo, error) { return os.Stat(w.launchFile) }
