package domain

import "time"

// The scores of a bench run (ADR 0015). Glossary: "Scorecard".

// The patterns a case is scored on.
const (
	BenchPatternRightTool   = "right_tool"
	BenchPatternValidParams = "valid_params"
	BenchPatternRightOrder  = "right_order"
	BenchPatternOutcome     = "outcome"
	BenchPatternTrajectory  = "trajectory"
)

// BenchPatterns are the patterns in the order a scorecard shows them.
var BenchPatterns = []string{BenchPatternRightTool, BenchPatternValidParams, BenchPatternRightOrder, BenchPatternOutcome, BenchPatternTrajectory}

// BenchPatternScore is one pattern's score for a case: 0 to 1, with why it
// lost points. A pattern the case expects nothing of, or one that could not be
// judged, is not Scored.
type BenchPatternScore struct {
	Scored  bool     `json:"scored"`
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

// BenchToolUseScores are the tool-use patterns of one case.
type BenchToolUseScores struct {
	RightTool   BenchPatternScore `json:"right_tool"`
	ValidParams BenchPatternScore `json:"valid_params"`
	RightOrder  BenchPatternScore `json:"right_order"`
}

// BenchStepNote is the judge's note on one step of a trajectory.
type BenchStepNote struct {
	Step int    `json:"step"`
	OK   bool   `json:"ok"`
	Note string `json:"note"`
}

// BenchCaseScore is how one case went in a run.
type BenchCaseScore struct {
	CaseID     string             `json:"case_id"`
	CaseName   string             `json:"case_name,omitempty"`
	ToolUse    BenchToolUseScores `json:"tool_use"`
	Outcome    BenchPatternScore  `json:"outcome"`
	Trajectory BenchPatternScore  `json:"trajectory"`
	Steps      []BenchStepNote    `json:"steps,omitempty"`
	Calls      []BenchCall        `json:"calls"`
	FinalText  string             `json:"final_text,omitempty"`
	CostUSD    float64            `json:"cost_usd"`
	DurationMS int64              `json:"duration_ms"`
	// Error is why the case could not run, "" when it did.
	Error string `json:"error,omitempty"`
}

// Pattern is the case's score on a pattern, by the names in BenchPatterns.
func (c BenchCaseScore) Pattern(name string) BenchPatternScore {
	switch name {
	case BenchPatternRightTool:
		return c.ToolUse.RightTool
	case BenchPatternValidParams:
		return c.ToolUse.ValidParams
	case BenchPatternRightOrder:
		return c.ToolUse.RightOrder
	case BenchPatternOutcome:
		return c.Outcome
	case BenchPatternTrajectory:
		return c.Trajectory
	}
	return BenchPatternScore{}
}

// Mean is the mean of the patterns that are scored; ok is false when none is.
func (c BenchCaseScore) Mean() (mean float64, ok bool) {
	var sum float64
	n := 0
	for _, p := range BenchPatterns {
		if s := c.Pattern(p); s.Scored {
			sum += s.Score
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// BenchAggregate is a mean over the cases that were scored.
type BenchAggregate struct {
	Scored int     `json:"scored"`
	Mean   float64 `json:"mean"`
}

// Scorecard statuses.
const (
	ScorecardFinished  = "finished"
	ScorecardCancelled = "cancelled"
)

// BenchSetupRecord is the setup a scorecard ran on: the fingerprint that
// lets a difference be traced to a change, and the parts that name it.
type BenchSetupRecord struct {
	Fingerprint string `json:"fingerprint"`
	AgentName   string `json:"agent_name,omitempty"`
	Model       string `json:"model,omitempty"`
	JudgeModel  string `json:"judge_model,omitempty"`
	CLIVersion  string `json:"cli_version,omitempty"`
}

// Scorecard is the result of one bench run of a project: scores per case, per
// pattern and in aggregate, with cost and duration.
type Scorecard struct {
	ID         string                    `json:"id"`
	ProjectID  string                    `json:"project_id"`
	Status     string                    `json:"status"`
	Setup      BenchSetupRecord          `json:"setup"`
	Cases      []BenchCaseScore          `json:"cases"`
	Patterns   map[string]BenchAggregate `json:"patterns"`
	Aggregate  BenchAggregate            `json:"aggregate"`
	CostUSD    float64                   `json:"cost_usd"`
	DurationMS int64                     `json:"duration_ms"`
	StartedAt  time.Time                 `json:"started_at"`
	FinishedAt time.Time                 `json:"finished_at"`
}

// BenchDelta is a score before and after, and the change.
type BenchDelta struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Change float64 `json:"change"`
}

// How a case stands between two scorecards.
const (
	BenchCaseChanged   = "changed"
	BenchCaseUnchanged = "unchanged"
	BenchCaseAdded     = "added"
	BenchCaseRemoved   = "removed"
)

// BenchCaseDelta is one case between two scorecards. An added or removed case
// has no delta.
type BenchCaseDelta struct {
	CaseID   string                `json:"case_id"`
	CaseName string                `json:"case_name,omitempty"`
	Status   string                `json:"status"`
	Mean     *BenchDelta           `json:"mean,omitempty"`
	Patterns map[string]BenchDelta `json:"patterns,omitempty"`
}

// ScorecardComparison is a scorecard against the previous run of the same
// project.
type ScorecardComparison struct {
	PreviousID         string                `json:"previous_id"`
	FingerprintChanged bool                  `json:"fingerprint_changed"`
	Aggregate          BenchDelta            `json:"aggregate"`
	Patterns           map[string]BenchDelta `json:"patterns"`
	Cases              []BenchCaseDelta      `json:"cases"`
}
