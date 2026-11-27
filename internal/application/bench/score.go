package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"operators-mcp/internal/domain"
)

// The two questions the judge model is asked. The reference and the result, or
// the case and its trace, go in the input and never in these, so text in them
// cannot redirect the instruction.
const (
	similaritySystem = `You compare two texts for meaning. The input has a REFERENCE and a RESULT. ` +
		`Judge how closely the RESULT says what the REFERENCE says, ignoring wording and length. ` +
		`Answer with one JSON object and nothing else: {"similarity": <number from 0 to 1>}.`
	trajectorySystem = `You judge an agent's trajectory step by step. The input has the TASK, what the agent was ` +
		`expected to do, and the numbered STEPS (tool calls with their parameters) it took, then its FINAL result. ` +
		`Judge each step: was it a sensible move toward the task? Answer with one JSON object and nothing else: ` +
		`{"score": <number from 0 to 1 for the whole trajectory>, "steps": [{"step": <number>, "ok": <true|false>, "note": "<why>"}]}.`
)

// ScoreCase scores one case's run: the tool-use patterns from the recorded
// calls, the outcome by the judge model's similarity of the final result to the
// reference, and the trajectory by its step-by-step judgment. A judgment that
// fails or cannot be read leaves that pattern unscored with the reason; the
// rest of the case is still scored.
func (s *Service) ScoreCase(ctx context.Context, c domain.BenchCase, run CaseRun, judgeModel string) domain.BenchCaseScore {
	score := domain.BenchCaseScore{
		CaseID: c.ID, CaseName: c.Name, ToolUse: ScoreToolUse(c, run.Calls),
		Calls: run.Calls, FinalText: run.FinalText, CostUSD: run.CostUSD, DurationMS: run.Duration.Milliseconds(),
	}
	if score.Calls == nil {
		score.Calls = []domain.BenchCall{}
	}
	if strings.TrimSpace(c.Expect.Reference) != "" {
		score.Outcome = s.similarity(ctx, judgeModel, c.Expect.Reference, run.FinalText)
	}
	score.Trajectory, score.Steps = s.trajectory(ctx, judgeModel, c, run)
	return score
}

func (s *Service) similarity(ctx context.Context, model, reference, result string) domain.BenchPatternScore {
	input := "REFERENCE:\n" + reference + "\n\nRESULT:\n" + result
	var out struct {
		Similarity *float64 `json:"similarity"`
	}
	if err := s.ask(ctx, model, similaritySystem, input, &out); err != nil {
		return unavailable(err)
	}
	if out.Similarity == nil || !inRange(*out.Similarity) {
		return unavailable(errors.New("the judge's similarity is missing or outside 0 to 1"))
	}
	return domain.BenchPatternScore{Scored: true, Score: *out.Similarity}
}

func (s *Service) trajectory(ctx context.Context, model string, c domain.BenchCase, run CaseRun) (domain.BenchPatternScore, []domain.BenchStepNote) {
	var b strings.Builder
	fmt.Fprintf(&b, "TASK:\n%s\n\nEXPECTED:\n", c.Prompt)
	for i, e := range c.Expect.Tools {
		p, _ := json.Marshal(e.Params)
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, e.Tool, p)
	}
	if c.Expect.Reference != "" {
		fmt.Fprintf(&b, "Reference result: %s\n", c.Expect.Reference)
	}
	b.WriteString("\nSTEPS:\n")
	for i, call := range run.Calls {
		p, _ := json.Marshal(call.Params)
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, call.Tool, p)
	}
	fmt.Fprintf(&b, "\nFINAL:\n%s\n", run.FinalText)

	var out struct {
		Score *float64               `json:"score"`
		Steps []domain.BenchStepNote `json:"steps"`
	}
	if err := s.ask(ctx, model, trajectorySystem, b.String(), &out); err != nil {
		return unavailable(err), nil
	}
	if out.Score == nil || !inRange(*out.Score) {
		return unavailable(errors.New("the judge's trajectory score is missing or outside 0 to 1")), nil
	}
	return domain.BenchPatternScore{Scored: true, Score: *out.Score}, out.Steps
}

// ask puts a question to the judge model and decodes the JSON object in its
// answer, which may have words around it.
func (s *Service) ask(ctx context.Context, model, system, input string, into any) error {
	if s.d.Judge == nil {
		return errors.New("no judge model is configured")
	}
	reply, err := s.d.Judge.Ask(ctx, model, system, input)
	if err != nil {
		return err
	}
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return errors.New("the judge did not answer with a JSON object")
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), into); err != nil {
		return fmt.Errorf("the judge's answer is not readable: %w", err)
	}
	return nil
}

func unavailable(err error) domain.BenchPatternScore {
	return domain.BenchPatternScore{Reasons: []string{"judge unavailable: " + err.Error()}}
}

func inRange(v float64) bool { return v >= 0 && v <= 1 && !math.IsNaN(v) }

// NewScorecard assembles a scorecard from the cases' scores: the mean per
// pattern over the cases that scored it, the aggregate as the mean of the
// cases' own means over the cases with any score (a case that could not run is a zero), and the total cost and
// duration. Its id and times are the caller's.
func NewScorecard(projectID string, setup domain.BenchSetupRecord, cases []domain.BenchCaseScore, status string) domain.Scorecard {
	card := domain.Scorecard{ProjectID: projectID, Status: status, Setup: setup, Cases: cases, Patterns: map[string]domain.BenchAggregate{}}
	if card.Cases == nil {
		card.Cases = []domain.BenchCaseScore{}
	}
	sums := map[string]float64{}
	var total float64
	for _, c := range cases {
		card.CostUSD += c.CostUSD
		card.DurationMS += c.DurationMS
		for _, name := range domain.BenchPatterns {
			if p := c.Pattern(name); p.Scored {
				a := card.Patterns[name]
				a.Scored++
				card.Patterns[name] = a
				sums[name] += p.Score
			}
		}
		if mean, ok := c.Mean(); ok {
			card.Aggregate.Scored++
			total += mean
		}
	}
	for name, a := range card.Patterns {
		a.Mean = sums[name] / float64(a.Scored)
		card.Patterns[name] = a
	}
	if card.Aggregate.Scored > 0 {
		card.Aggregate.Mean = total / float64(card.Aggregate.Scored)
	}
	return card
}

// benchGroups are the scorecard's three columns and the patterns each is made of.
var benchGroups = map[string][]string{
	"tool_use":   {domain.BenchPatternRightTool, domain.BenchPatternValidParams, domain.BenchPatternRightOrder},
	"outcome":    {domain.BenchPatternOutcome},
	"trajectory": {domain.BenchPatternTrajectory},
}

// groupMean is a scorecard's mean over the patterns, each weighted by the cases
// scored on it; ok is false when none was scored.
func groupMean(card domain.Scorecard, patterns []string) (mean float64, ok bool) {
	var weight, sum float64
	for _, name := range patterns {
		a, has := card.Patterns[name]
		if !has || a.Scored == 0 {
			continue
		}
		weight += float64(a.Scored)
		sum += a.Mean * float64(a.Scored)
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}

const epsilon = 1e-9

func delta(before, after float64) domain.BenchDelta {
	return domain.BenchDelta{Before: before, After: after, Change: after - before}
}

// Compare sets a scorecard against the previous run of the same project: per
// case (by case id), per pattern and in aggregate. A case in only one of them
// is listed as added or removed and carries no delta; a changed fingerprint is
// reported so a difference can be traced to the setup.
func Compare(prev, cur domain.Scorecard) domain.ScorecardComparison {
	out := domain.ScorecardComparison{
		PreviousID:         prev.ID,
		FingerprintChanged: prev.Setup.Fingerprint != cur.Setup.Fingerprint,
		ChangedParts:       changedParts(prev.Setup, cur.Setup),
		Aggregate:          delta(prev.Aggregate.Mean, cur.Aggregate.Mean),
		Patterns:           map[string]domain.BenchDelta{},
		Groups:             map[string]domain.BenchDelta{},
		Cases:              []domain.BenchCaseDelta{},
	}
	for _, name := range domain.BenchPatterns {
		p, hadPrev := prev.Patterns[name]
		c, hasCur := cur.Patterns[name]
		if hadPrev || hasCur {
			out.Patterns[name] = delta(p.Mean, c.Mean)
		}
	}
	for name, patterns := range benchGroups {
		p, hadPrev := groupMean(prev, patterns)
		c, hasCur := groupMean(cur, patterns)
		if hadPrev || hasCur {
			out.Groups[name] = delta(p, c)
		}
	}
	before := map[string]domain.BenchCaseScore{}
	for _, c := range prev.Cases {
		before[c.CaseID] = c
	}
	seen := map[string]bool{}
	for _, c := range cur.Cases {
		seen[c.CaseID] = true
		p, ok := before[c.CaseID]
		if !ok {
			out.Cases = append(out.Cases, domain.BenchCaseDelta{CaseID: c.CaseID, CaseName: c.CaseName, Status: domain.BenchCaseAdded})
			continue
		}
		out.Cases = append(out.Cases, compareCase(p, c))
	}
	for _, p := range prev.Cases {
		if !seen[p.CaseID] {
			out.Cases = append(out.Cases, domain.BenchCaseDelta{CaseID: p.CaseID, CaseName: p.CaseName, Status: domain.BenchCaseRemoved})
		}
	}
	return out
}

func compareCase(prev, cur domain.BenchCaseScore) domain.BenchCaseDelta {
	d := domain.BenchCaseDelta{CaseID: cur.CaseID, CaseName: cur.CaseName, Status: domain.BenchCaseUnchanged, Error: cur.Error, Patterns: map[string]domain.BenchDelta{}}
	for _, name := range domain.BenchPatterns {
		p, c := prev.Pattern(name), cur.Pattern(name)
		if p.Scored && c.Scored && math.Abs(c.Score-p.Score) > epsilon {
			d.Patterns[name] = delta(p.Score, c.Score)
			d.Status = domain.BenchCaseChanged
		}
	}
	pm, pok := prev.Mean()
	cm, cok := cur.Mean()
	if pok && cok && math.Abs(cm-pm) > epsilon {
		m := delta(pm, cm)
		d.Mean = &m
	}
	// A case that could not run in one run and ran in the other is a change
	// whatever its patterns say: it started failing, or stopped.
	if (prev.Error == "") != (cur.Error == "") {
		d.Status = domain.BenchCaseChanged
	}
	return d
}
