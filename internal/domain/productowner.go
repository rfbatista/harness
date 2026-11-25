package domain

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// A project's product owner (ADR 0109) is a per-project concept: its settings
// belong to the project, and each conversation with it is a thread.

// ProductOwnerMaxPrompt and ProductOwnerMaxMessage bound the project prompt
// and one message of a thread, in characters.
const (
	ProductOwnerMaxPrompt  = 20000
	ProductOwnerMaxMessage = 20000
	// ProductOwnerMaxAttachments is how many attachments one message takes.
	ProductOwnerMaxAttachments = 5
	// ProductOwnerThreadPage is how many items one read of a thread answers.
	ProductOwnerThreadPage = 100
)

// ProductOwnerSettings are a project's product owner settings. The zero value
// is what a project has until the person changes something: switched on, no
// agent, no prompt, no digest time.
type ProductOwnerSettings struct {
	ProjectID string
	// Enabled is false once the person switched the product owner off.
	Enabled bool
	// AgentID is the catalog agent the product owner runs as; empty is plain
	// claude with the mode's built-in skill.
	AgentID string
	Prompt  string
	// DigestTime is "HH:MM", empty while no digest is recorded.
	DigestTime string
}

// DefaultProductOwnerSettings are a project's settings before any change.
func DefaultProductOwnerSettings(projectID string) ProductOwnerSettings {
	return ProductOwnerSettings{ProjectID: projectID, Enabled: true}
}

var digestTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidDigestTime is nil when s is "HH:MM" on a 24-hour clock, or empty.
func ValidDigestTime(s string) error {
	if s == "" || digestTimePattern.MatchString(s) {
		return nil
	}
	return &StructuredError{Code: "INVALID_INPUT", Message: `digest_time must be "HH:MM" on a 24-hour clock, or empty`}
}

// Turn states of a product-owner thread.
const (
	ProductOwnerTurnIdle = "idle"
	// ProductOwnerTurnResuming is a thread whose session is waking up before
	// its turn starts.
	ProductOwnerTurnResuming    = "resuming"
	ProductOwnerTurnRunning     = "running"
	ProductOwnerTurnInterrupted = "interrupted"
)

// ProductOwnerThread is one conversation with a project's product owner. Its
// session is the headless claude session in product-owner mode that answers.
type ProductOwnerThread struct {
	ID             string    `json:"thread_id"`
	ProjectID      string    `json:"project_id"`
	SessionID      string    `json:"session_id"`
	Title          string    `json:"title"`
	Turn           string    `json:"turn"`
	StartedAt      time.Time `json:"started_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	// LastMessageExcerpt is the first line of the thread's last message, at
	// most 160 characters; TitleSetByPerson says the person renamed the thread.
	LastMessageExcerpt string `json:"last_message_excerpt"`
	TitleSetByPerson   bool   `json:"title_set_by_person"`
	// PendingHeldActions counts the held actions waiting on the person.
	PendingHeldActions int `json:"pending_held_actions"`
	// Checkpoints counts the checkpoints the thread has made (ADR 0112).
	Checkpoints int `json:"checkpoints"`
	// CheckpointPhase says a checkpoint is under way: the summary is being
	// written, or stored and the thread is moving to its new conversation.
	CheckpointPhase string `json:"-"`
	// CheckpointSummary is the summary of the thread's latest checkpoint, ""
	// before its first.
	CheckpointSummary string `json:"-"`
	// AgentID and Prompt are what the thread started with: a running thread
	// keeps them while the project's settings change (ADR 0099).
	AgentID string `json:"-"`
	Prompt  string `json:"-"`
	// MemoryOpened says the thread has been handed the project's memory (ADR
	// 0111); MemorySeen is the memory version it was handed or last told of.
	MemoryOpened bool  `json:"-"`
	MemorySeen   int64 `json:"-"`
}

// Kinds of thread item. Later pieces add kinds; the thread store keeps each
// item as its kind and its fields, apart from the claude transcript.
const (
	ProductOwnerItemMessage  = "message"
	ProductOwnerItemActivity = "activity"
	ProductOwnerItemNotice   = "notice"
	ProductOwnerItemFailure  = "failure"
	// ProductOwnerItemCheckpoint marks where a thread checkpointed: the items
	// before it are its archived segment and Summary stands for them.
	ProductOwnerItemCheckpoint = "checkpoint"
	// ProductOwnerItemMemoryNote says the product owner saved, changed or
	// dropped a memory entry (ADR 0111).
	ProductOwnerItemMemoryNote = "memory_note"
)

// Authors of a message item.
const (
	ProductOwnerAuthorPerson       = "person"
	ProductOwnerAuthorProductOwner = "product_owner"
)

// ProductOwnerItem is one entry of a thread. Fields depend on Kind, as the
// contract lists them; Seq orders the items of a thread and is not on the wire.
type ProductOwnerItem struct {
	ID       string    `json:"item_id"`
	ThreadID string    `json:"-"`
	Seq      int64     `json:"-"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
	// A message's fields.
	Author    string `json:"author,omitempty"`
	Text      string `json:"text,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`
	// Stopped marks a message the person stopped while it streamed.
	Stopped bool `json:"stopped,omitempty"`
	// A failure's fields: Text says what happened, RetryItemID is the person's
	// message to send again.
	Code        string `json:"code,omitempty"`
	RetryItemID string `json:"retry_item_id,omitempty"`
	// A checkpoint's summary.
	Summary string `json:"summary,omitempty"`
	// A memory note's fields (its text is the entry's): Op is saved, changed or
	// dropped.
	Op      string `json:"op,omitempty"`
	EntryID string `json:"entry_id,omitempty"`
}

// ProductOwnerMaxTitle bounds a thread title the person sets, in characters.
const ProductOwnerMaxTitle = 200

// ProductOwnerExcerpt is the first line of a message, at most 160 characters.
func ProductOwnerExcerpt(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > 160 {
		line = strings.TrimSpace(string(r[:159])) + "…"
	}
	return line
}

// MarshalJSON writes the item as the contract shapes it: a message always
// carries its attachments and its streaming flag.
func (i ProductOwnerItem) MarshalJSON() ([]byte, error) {
	type plain ProductOwnerItem
	if i.Kind != ProductOwnerItemMessage {
		return json.Marshal(plain(i))
	}
	return json.Marshal(struct {
		plain
		Text        string `json:"text"`
		Attachments []any  `json:"attachments"`
		Streaming   bool   `json:"streaming"`
		Stopped     bool   `json:"stopped"`
	}{plain: plain(i), Text: i.Text, Attachments: []any{}, Streaming: i.Streaming, Stopped: i.Stopped})
}

// ProductOwnerThreadTitle is a thread's title until the product owner names
// it: the first message cut to one line.
func ProductOwnerThreadTitle(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	line = strings.TrimSpace(line)
	const max = 80
	if r := []rune(line); len(r) > max {
		line = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return line
}

// Phases of a checkpoint (ADR 0112), kept on the thread so that a restart
// finishes it.
const (
	// ProductOwnerCheckpointSummarizing is the summary turn: the product owner
	// is writing the summary of what is archived.
	ProductOwnerCheckpointSummarizing = "summarizing"
	// ProductOwnerCheckpointMoving is the summary stored with its checkpoint
	// item, the thread not yet in its new conversation.
	ProductOwnerCheckpointMoving = "moving"
)

// ProductOwnerCheckpointShare is how much of its model's window a thread's
// context may fill before the thread checkpoints.
const ProductOwnerCheckpointShare = 0.6

// ContextWindow is the size in tokens of a model's context window: a model
// named with the 1M suffix has a million, any other 200,000.
func ContextWindow(model string) int {
	if strings.Contains(strings.ToLower(model), "[1m]") {
		return 1_000_000
	}
	return 200_000
}

// ProductOwnerSearchLimit is how many matches a search answers, and
// ProductOwnerMaxSearch how long a query may be, in characters.
const (
	ProductOwnerSearchLimit = 30
	ProductOwnerMaxSearch   = 200
)

// ProductOwnerSearchMatch is a passage of a thread found by a search. Archived
// says it lies in the part of its thread before a checkpoint; Snippet has the
// matched words between « and ».
type ProductOwnerSearchMatch struct {
	ThreadID    string    `json:"thread_id"`
	ThreadTitle string    `json:"thread_title"`
	ItemID      string    `json:"item_id"`
	At          time.Time `json:"at"`
	Archived    bool      `json:"archived"`
	Snippet     string    `json:"snippet"`
}
