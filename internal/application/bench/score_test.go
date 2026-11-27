package bench

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// scriptedJudge answers the similarity and the trajectory question with what
// the test scripts; nothing here calls a model.
type scriptedJudge struct {
	similarity, trajectory string
	err                    error
	models                 []string
}

func (j *scriptedJudge) Ask(_ context.Context, model, system, _ string) (string, error) {
	j.models = append(j.models, model)
	if j.err != nil {
		return "", j.err
	}
	if system == similaritySystem {
		return j.similarity, nil
	}
	return j.trajectory, nil
}

func scoring(j *scriptedJudge) *Service { return New(Deps{Judge: j}) }

var demoCase = domain.BenchCase{
	ID:     "c1",
	Name:   "reads the readme",
	Prompt: "Read the README.",
	Tools:  []domain.BenchTool{{Name: "read_file", Result: "# Demo"}},
	Expect: domain.BenchExpectation{
		Tools:     []domain.BenchExpectedCall{{Tool: "read_file", Params: map[string]any{"path": "README.md"}}},
		Ordered:   true,
		Reference: "A demo project.",
	},
}

var demoRun = CaseRun{
	Calls:     []domain.BenchCall{{Tool: "read_file", Params: map[string]any{"path": "README.md"}}},
	FinalText: "It is a demo project.",
	CostUSD:   0.02,
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAScoredCaseCarriesOutcomeTrajectoryAndTheJudgesStepNotes(t *testing.T) {
	judge := &scriptedJudge{
		similarity: `{"similarity": 0.8}`,
		trajectory: "Here it is: {\"score\": 0.5, \"steps\": [{\"step\": 1, \"ok\": true, \"note\": \"read first\"}, {\"step\": 2, \"ok\": false, \"note\": \"never answered\"}]}",
	}
	got := scoring(judge).ScoreCase(context.Background(), demoCase, demoRun, "opus")

	if !got.Outcome.Scored || !near(got.Outcome.Score, 0.8) {
		t.Fatalf("outcome = %+v", got.Outcome)
	}
	if !got.Trajectory.Scored || !near(got.Trajectory.Score, 0.5) {
		t.Fatalf("trajectory = %+v", got.Trajectory)
	}
	if len(got.Steps) != 2 || got.Steps[1].OK || got.Steps[1].Note != "never answered" {
		t.Fatalf("steps = %+v", got.Steps)
	}
	if !got.ToolUse.RightTool.Scored || got.ToolUse.RightTool.Score != 1 {
		t.Fatalf("tool use = %+v", got.ToolUse)
	}
	for _, m := range judge.models {
		if m != "opus" {
			t.Fatalf("the judge ran on %q, want the judge model", m)
		}
	}
	if got.CaseID != "c1" || got.CostUSD != 0.02 || len(got.Calls) != 1 {
		t.Fatalf("case = %+v", got)
	}
}

func TestACaseWithoutAReferenceHasNoOutcomeScoreAndAsksNoSimilarity(t *testing.T) {
	c := demoCase
	c.Expect.Reference = ""
	judge := &scriptedJudge{trajectory: `{"score": 1, "steps": []}`}
	got := scoring(judge).ScoreCase(context.Background(), c, demoRun, "opus")
	if got.Outcome.Scored {
		t.Fatalf("outcome = %+v", got.Outcome)
	}
	if len(judge.models) != 1 {
		t.Fatalf("the judge was asked %d times, want only the trajectory", len(judge.models))
	}
}

func TestAJudgeFailureMakesThoseScoresUnavailableAndKeepsTheRest(t *testing.T) {
	judge := &scriptedJudge{err: errors.New("model down")}
	got := scoring(judge).ScoreCase(context.Background(), demoCase, demoRun, "opus")

	for name, p := range map[string]domain.BenchPatternScore{"outcome": got.Outcome, "trajectory": got.Trajectory} {
		if p.Scored || len(p.Reasons) == 0 || !strings.Contains(p.Reasons[0], "model down") {
			t.Errorf("%s = %+v, want unscored with the reason", name, p)
		}
	}
	if !got.ToolUse.RightTool.Scored {
		t.Fatal("the tool-use scores were lost with the judge")
	}

	judge = &scriptedJudge{similarity: "no json here", trajectory: `{"score": 2}`}
	got = scoring(judge).ScoreCase(context.Background(), demoCase, demoRun, "opus")
	if got.Outcome.Scored || got.Trajectory.Scored {
		t.Fatalf("an unreadable or out-of-range answer was scored: %+v %+v", got.Outcome, got.Trajectory)
	}
}

func caseScore(id string, right, outcome float64, scoredOutcome bool) domain.BenchCaseScore {
	return domain.BenchCaseScore{
		CaseID: id, CostUSD: 0.5, DurationMS: 1000,
		ToolUse: domain.BenchToolUseScores{RightTool: domain.BenchPatternScore{Scored: true, Score: right}},
		Outcome: domain.BenchPatternScore{Scored: scoredOutcome, Score: outcome},
	}
}

func TestScorecardAggregatesPerPatternAndOverallAcrossScoredAndUnscoredCases(t *testing.T) {
	card := NewScorecard("p1", domain.BenchSetupRecord{Fingerprint: "f"}, []domain.BenchCaseScore{
		caseScore("a", 1, 0.5, true),
		caseScore("b", 0.5, 0, false), // no outcome expected
		{CaseID: "c"},                 // nothing scored at all
	}, domain.ScorecardFinished)

	if got := card.Patterns[domain.BenchPatternRightTool]; got.Scored != 2 || !near(got.Mean, 0.75) {
		t.Fatalf("right_tool = %+v", got)
	}
	if got := card.Patterns[domain.BenchPatternOutcome]; got.Scored != 1 || !near(got.Mean, 0.5) {
		t.Fatalf("outcome = %+v", got)
	}
	// Per case: a = (1+0.5)/2, b = 0.5; c is not scored.
	if card.Aggregate.Scored != 2 || !near(card.Aggregate.Mean, (0.75+0.5)/2) {
		t.Fatalf("aggregate = %+v", card.Aggregate)
	}
	if !near(card.CostUSD, 1) || card.DurationMS != 2000 {
		t.Fatalf("cost %v duration %d", card.CostUSD, card.DurationMS)
	}
	if card.Setup.Fingerprint != "f" || card.ProjectID != "p1" || card.Status != domain.ScorecardFinished {
		t.Fatalf("card = %+v", card)
	}
}

func cardOf(fingerprint string, cases ...domain.BenchCaseScore) domain.Scorecard {
	return NewScorecard("p1", domain.BenchSetupRecord{Fingerprint: fingerprint}, cases, domain.ScorecardFinished)
}

func TestComparingTwoScorecardsOnIdenticalInputsShowsNoDifference(t *testing.T) {
	prev := cardOf("f", caseScore("a", 1, 0.5, true), caseScore("b", 0.5, 0, false))
	prev.ID = "old"
	cur := cardOf("f", caseScore("a", 1, 0.5, true), caseScore("b", 0.5, 0, false))

	got := Compare(prev, cur)
	if got.PreviousID != "old" || got.FingerprintChanged || got.Aggregate.Change != 0 {
		t.Fatalf("comparison = %+v", got)
	}
	for _, c := range got.Cases {
		if c.Status != domain.BenchCaseUnchanged {
			t.Fatalf("case %s is %s", c.CaseID, c.Status)
		}
	}
}

func TestComparingShowsAChangedCaseItsDeltaAndTheChangedFingerprint(t *testing.T) {
	prev := cardOf("f1", caseScore("a", 1, 0.5, true))
	cur := cardOf("f2", caseScore("a", 1, 0.9, true))

	got := Compare(prev, cur)
	if !got.FingerprintChanged {
		t.Fatal("the changed fingerprint is not reported")
	}
	if len(got.Cases) != 1 || got.Cases[0].Status != domain.BenchCaseChanged {
		t.Fatalf("cases = %+v", got.Cases)
	}
	c := got.Cases[0]
	if d := c.Patterns[domain.BenchPatternOutcome]; !near(d.Change, 0.4) || !near(d.Before, 0.5) || !near(d.After, 0.9) {
		t.Fatalf("outcome delta = %+v", d)
	}
	if _, moved := c.Patterns[domain.BenchPatternRightTool]; moved {
		t.Fatal("an unchanged pattern is listed as a delta")
	}
	if c.Mean == nil || !near(c.Mean.Change, 0.2) {
		t.Fatalf("mean delta = %+v", c.Mean)
	}
	if !near(got.Aggregate.Change, 0.2) || !near(got.Patterns[domain.BenchPatternOutcome].Change, 0.4) {
		t.Fatalf("aggregate %+v patterns %+v", got.Aggregate, got.Patterns)
	}
}

func TestAnAddedOrRemovedCaseIsListedAsSuchNotAsADelta(t *testing.T) {
	prev := cardOf("f", caseScore("a", 1, 0.5, true), caseScore("gone", 1, 1, true))
	cur := cardOf("f", caseScore("a", 1, 0.5, true), caseScore("new", 0, 0, true))

	status := map[string]string{}
	for _, c := range Compare(prev, cur).Cases {
		status[c.CaseID] = c.Status
		if (c.Status == domain.BenchCaseAdded || c.Status == domain.BenchCaseRemoved) && (c.Mean != nil || len(c.Patterns) != 0) {
			t.Fatalf("%s %s carries a delta", c.CaseID, c.Status)
		}
	}
	if status["a"] != domain.BenchCaseUnchanged || status["gone"] != domain.BenchCaseRemoved || status["new"] != domain.BenchCaseAdded {
		t.Fatalf("status = %v", status)
	}
}
