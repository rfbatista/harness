package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A gate run (Glossary "Gate run"): the harness running a session's gates in
// its worktree, without the agent (ADR 0012, 0014), and what it recorded.

// GateTrigger says what asked for a gate run.
type GateTrigger string

const (
	// GateTriggerDeclaration is the session's done declaration.
	GateTriggerDeclaration GateTrigger = "declaration"
	// GateTriggerPractice is the agent's own run_gates; it never counts.
	GateTriggerPractice GateTrigger = "practice"
	// GateTriggerPerson is the person's request from the agent box.
	GateTriggerPerson GateTrigger = "person"
)

// GateRunResult is how a gate run ended.
type GateRunResult string

const (
	GateRunGreen GateRunResult = "green"
	GateRunRed   GateRunResult = "red"
	// GateRunNotPassed is a run that could not start: a required gate has no
	// command, so nothing ran.
	GateRunNotPassed GateRunResult = "not_passed"
)

// MaxGateOutputBytes is the output a gate keeps: its tail, as the agent needs
// the end of a failing run.
const MaxGateOutputBytes = 256 << 10

// GateResult is one gate of a gate run.
type GateResult struct {
	Kind           GateKind `json:"kind"`
	Command        string   `json:"command"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	ExitCode       int      `json:"exit_code"`
	TimedOut       bool     `json:"timed_out,omitempty"`
	DurationMs     int64    `json:"duration_ms"`
	Output         string   `json:"output"`
	// Skipped is a gate not run because an earlier one stopped the run.
	Skipped bool `json:"skipped,omitempty"`
}

// Passed says the gate ran and exited 0 in time.
func (g GateResult) Passed() bool { return !g.Skipped && !g.TimedOut && g.ExitCode == 0 }

// OutputTail is the last n bytes of the gate's output.
func (g GateResult) OutputTail(n int) string {
	if len(g.Output) <= n {
		return g.Output
	}
	return "…" + TrimToRuneStart(g.Output[len(g.Output)-n:])
}

// TrimToRuneStart drops the bytes at the front of s that are the tail of a
// character cut in half, so s is valid UTF-8 where it was before the cut.
func TrimToRuneStart(s string) string {
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// GateRun is one gate run of a session, as recorded.
type GateRun struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	Trigger   GateTrigger   `json:"trigger"`
	Result    GateRunResult `json:"result"`
	// Counted says the run counts toward the retry limit: a declaration run.
	Counted bool `json:"counted"`
	// Escalated says this red declaration reached the retry limit and stopped
	// the session.
	Escalated bool `json:"escalated,omitempty"`
	// Kinds is the filter a practice run was limited to; empty is every gate.
	Kinds []GateKind `json:"kinds,omitempty"`
	// MissingKinds are the required gates with no command, for a not passed run.
	MissingKinds []GateKind   `json:"missing_kinds,omitempty"`
	Gates        []GateResult `json:"gates"`
	StartedAt    time.Time    `json:"started_at"`
	FinishedAt   time.Time    `json:"finished_at"`
}

// Finish settles the run's result from its gates: red when any gate failed,
// timed out or was skipped, green otherwise. A run with missing kinds is not
// passed whatever its gates say.
func (r *GateRun) Finish(now time.Time) {
	r.FinishedAt = now
	r.Counted = r.Trigger == GateTriggerDeclaration
	switch {
	case len(r.MissingKinds) > 0:
		r.Result = GateRunNotPassed
	default:
		r.Result = GateRunGreen
		for _, g := range r.Gates {
			if !g.Passed() {
				r.Result = GateRunRed
			}
		}
	}
}

// Green says the run passed.
func (r *GateRun) Green() bool { return r.Result == GateRunGreen }

// Failing are the gates that did not pass.
func (r *GateRun) Failing() []GateResult {
	var out []GateResult
	for _, g := range r.Gates {
		if !g.Passed() && !g.Skipped {
			out = append(out, g)
		}
	}
	return out
}

// MissingGatesMessage names the kinds a required gate is missing for.
func MissingGatesMessage(kinds []GateKind) string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	return "required gates with no command: " + strings.Join(names, ", ")
}

// MaxReportedOutputBytes is the part of a failing gate's output a refusal
// carries to the agent; the whole tail stays in the recorded run.
const MaxReportedOutputBytes = 4 << 10

// FailureReport tells an agent what failed in a run: the missing gate kinds,
// or each failing gate's command, exit code (or timeout), duration and the end
// of its output.
func (r *GateRun) FailureReport() string {
	if len(r.MissingKinds) > 0 {
		return MissingGatesMessage(r.MissingKinds)
	}
	var b strings.Builder
	for i, g := range r.Failing() {
		if i > 0 {
			b.WriteString("\n")
		}
		how := "exit code " + strconv.Itoa(g.ExitCode)
		if g.TimedOut {
			how = "timed out after " + strconv.Itoa(g.TimeoutSeconds) + "s"
		}
		b.WriteString("gate " + string(g.Kind) + " failed (" + how + ", " + (time.Duration(g.DurationMs) * time.Millisecond).String() + "): " + g.Command + "\n")
		b.WriteString(g.OutputTail(MaxReportedOutputBytes) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RedKinds are the kinds of the gates that did not pass.
func (r *GateRun) RedKinds() []GateKind {
	var out []GateKind
	for _, g := range r.Failing() {
		out = append(out, g.Kind)
	}
	return out
}

// TestFailed says a test gate of the run did not pass: it failed, timed out or
// was skipped by nothing, so a failed test run is a red `test` gate (the
// tests-fail-first rule's evidence).
func (r *GateRun) TestFailed() bool {
	for _, g := range r.Gates {
		if g.Kind == GateTest && !g.Passed() && !g.Skipped {
			return true
		}
	}
	return false
}
