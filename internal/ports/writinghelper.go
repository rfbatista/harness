package ports

import "context"

// The writing helper (ADR 0026): analyze, draft and titles steps run as
// stateless one-shots, and the browser holds the conversation.

// WritingHelperField is the text the person is writing and what it is
// (task_title, task_description or comment).
type WritingHelperField struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// WritingHelperSibling is another field of the same form.
type WritingHelperSibling struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// WritingHelperAnswer is a question the person answered.
type WritingHelperAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// WritingHelperRequest is one step, as the browser sends it.
type WritingHelperRequest struct {
	ProjectID string                 `json:"project_id"`
	Step      string                 `json:"step"`
	Field     WritingHelperField     `json:"field"`
	Siblings  []WritingHelperSibling `json:"siblings"`
	Answers   []WritingHelperAnswer  `json:"answers"`
	// Draft and Request make a draft step a refine.
	Draft   string   `json:"draft"`
	Request string   `json:"request"`
	Exclude []string `json:"exclude"`
}

// WritingHelperQuestion is what the analyze step asks.
type WritingHelperQuestion struct {
	ID    string   `json:"id"`
	Text  string   `json:"text"`
	Why   string   `json:"why"`
	Chips []string `json:"chips"`
}

// WritingHelperAnalyzeResult is the result of the analyze step.
type WritingHelperAnalyzeResult struct {
	AlreadyGood bool                    `json:"already_good"`
	Questions   []WritingHelperQuestion `json:"questions"`
}

// WritingHelperDraftResult is the result of the draft step: the whole draft.
type WritingHelperDraftResult struct {
	Text string `json:"text"`
}

// WritingHelperTitlesResult is the result of the titles step.
type WritingHelperTitlesResult struct {
	Titles []string `json:"titles"`
}

// WritingHelperDelta is a piece of a draft as it is written.
type WritingHelperDelta struct {
	Text string `json:"text"`
}

// The event names, which are also the SSE event names.
const (
	WritingHelperEventDelta  = "delta"
	WritingHelperEventResult = "result"
)

// WritingHelperEvent is something a step reports: a WritingHelperDelta (name
// "delta") or a result (name "result").
type WritingHelperEvent struct {
	Name string
	Data any
}

// WritingHelperAvailability says whether the helper can run now and, when it
// cannot, why and where to fix it.
type WritingHelperAvailability struct {
	Available bool
	Reason    string
	FixURL    string
}

// WritingHelper runs the writing helper's steps.
type WritingHelper interface {
	// Run runs one step and reports its events in order. A refusal before
	// anything ran (INVALID_INPUT, PROJECT_NOT_FOUND, WRITING_HELPER_UNAVAILABLE),
	// a failed step (WRITING_HELPER_FAILED) or a cancelled ctx is the
	// returned error. The details of an unavailable helper carry its fix URL.
	Run(ctx context.Context, req WritingHelperRequest, emit func(WritingHelperEvent)) error
	// Availability says whether a step could run now.
	Availability(ctx context.Context) WritingHelperAvailability
}
