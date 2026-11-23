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
	ProductOwnerTurnIdle        = "idle"
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
	// PendingHeldActions counts the held actions waiting on the person.
	PendingHeldActions int `json:"pending_held_actions"`
	// AgentID and Prompt are what the thread started with: a running thread
	// keeps them while the project's settings change (ADR 0099).
	AgentID string `json:"-"`
	Prompt  string `json:"-"`
}

// Kinds of thread item. Later pieces add kinds; the thread store keeps each
// item as its kind and its fields, apart from the claude transcript.
const (
	ProductOwnerItemMessage  = "message"
	ProductOwnerItemActivity = "activity"
	ProductOwnerItemNotice   = "notice"
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
		Attachments []any `json:"attachments"`
		Streaming   bool  `json:"streaming"`
		Stopped     bool  `json:"stopped"`
	}{plain: plain(i), Attachments: []any{}, Streaming: i.Streaming, Stopped: i.Stopped})
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
