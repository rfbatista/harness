package domain

import (
	"fmt"
	"strings"
	"time"
)

// The eval bench (ADR 0015): a project's bench cases, each a prompt with mock
// tools and what a good run does. Glossary: "Bench case".

// BenchCaseSource says where a bench case came from.
type BenchCaseSource string

const (
	// BenchSourceDefinition is a case the person wrote.
	BenchSourceDefinition BenchCaseSource = "definition"
	// BenchSourceTrajectory is a case made from a recorded trajectory.
	BenchSourceTrajectory BenchCaseSource = "trajectory"
)

// BenchTool is a mock tool a case offers: a bench run answers every call to
// it with Result (an error result when IsError).
type BenchTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Result      string `json:"result"`
	IsError     bool   `json:"is_error,omitempty"`
}

// BenchExpectedCall is one call a good run makes. A nil Params accepts any
// arguments; otherwise each named parameter must equal the given value.
type BenchExpectedCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params,omitempty"`
}

// BenchExpectation is what a good run does: the tools it calls (in order when
// Ordered), and/or a reference for its final result.
type BenchExpectation struct {
	Tools     []BenchExpectedCall `json:"tools,omitempty"`
	Ordered   bool                `json:"ordered,omitempty"`
	Reference string              `json:"reference,omitempty"`
}

// BenchCase is one evaluation case of a project's eval bench.
type BenchCase struct {
	ID        string           `json:"id"`
	ProjectID string           `json:"project_id"`
	Name      string           `json:"name,omitempty"`
	Prompt    string           `json:"prompt"`
	Tools     []BenchTool      `json:"tools"`
	Expect    BenchExpectation `json:"expect"`
	Source    BenchCaseSource  `json:"source"`
	CreatedAt time.Time        `json:"created_at"`
}

// BenchFieldError is one reason a definition is refused.
type BenchFieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// BenchCaseValidationError is a refused definition: nothing was stored.
type BenchCaseValidationError struct{ Errors []BenchFieldError }

func (e *BenchCaseValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "invalid bench case"
	}
	return fmt.Sprintf("invalid bench case: %s: %s", e.Errors[0].Field, e.Errors[0].Message)
}

// Validate checks a definition and answers every fault, or nil.
func (c BenchCase) Validate() *BenchCaseValidationError {
	var errs []BenchFieldError
	add := func(field, code, msg string) { errs = append(errs, BenchFieldError{field, code, msg}) }

	if strings.TrimSpace(c.Prompt) == "" {
		add("prompt", "REQUIRED", "a bench case needs a prompt")
	}
	mocked := map[string]bool{}
	for i, t := range c.Tools {
		field := fmt.Sprintf("tools[%d].name", i)
		switch {
		case strings.TrimSpace(t.Name) == "":
			add(field, "REQUIRED", "a mock tool needs a name")
		case mocked[t.Name]:
			add(field, "DUPLICATE_TOOL", "tool "+t.Name+" is mocked twice")
		}
		mocked[t.Name] = true
	}
	if len(c.Expect.Tools) == 0 && strings.TrimSpace(c.Expect.Reference) == "" {
		add("expect", "NO_EXPECTATION", "expect tools or a reference result")
	}
	for i, call := range c.Expect.Tools {
		if !mocked[call.Tool] {
			add(fmt.Sprintf("expect.tools[%d].tool", i), "UNKNOWN_TOOL", "tool "+call.Tool+" has no mock")
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return &BenchCaseValidationError{Errors: errs}
}

// BenchCall is one tool call a run made: the tool and the arguments it was
// given.
type BenchCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params,omitempty"`
}

// BenchTrajectoryStep is one tool call of a recorded session: the tool, the
// arguments it got and the result it answered with.
type BenchTrajectoryStep struct {
	Tool    string         `json:"tool"`
	Params  map[string]any `json:"params,omitempty"`
	Result  string         `json:"result"`
	IsError bool           `json:"is_error,omitempty"`
}

// BenchTrajectory is what a session of a task opted in to recording did: its
// prompt, its tool calls with parameters and results, and its final state. It
// is in the bench's case format, so it can become a case (Glossary: "Bench
// case"). Its id is the session's.
type BenchTrajectory struct {
	ID         string                `json:"id"`
	ProjectID  string                `json:"project_id"`
	TaskID     string                `json:"task_id"`
	SessionID  string                `json:"session_id"`
	Prompt     string                `json:"prompt"`
	Steps      []BenchTrajectoryStep `json:"steps"`
	FinalState string                `json:"final_state"`
	RecordedAt time.Time             `json:"recorded_at"`
}

// TaskTrajectoryRecording says whether a task's sessions are recorded: what
// the task sets itself (Explicit, nil when it sets nothing) and what holds.
type TaskTrajectoryRecording struct {
	TaskID   string `json:"task_id"`
	Enabled  bool   `json:"enabled"`
	Explicit *bool  `json:"explicit"`
	// Source is "task" when the task sets it, else the level of the effective
	// settings default it follows ("repository", "project" or "harness").
	Source string `json:"source"`
	// Default is the settings default the task follows when it sets nothing,
	// and DefaultSource the level it comes from: the Edit form's resolution
	// line names it beside what the task sets itself.
	Default       bool   `json:"default"`
	DefaultSource string `json:"default_source"`
}
