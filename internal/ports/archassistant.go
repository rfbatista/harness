package ports

import (
	"context"
	"encoding/json"

	"operators-mcp/internal/domain"
)

// The architecture assistant (ADR 0060): a conversation on a repository's
// Architecture page in which claude reads the code and proposes changes to the
// record. It only proposes; nothing is stored until the person saves.

// ArchitectureAssistantRequest is one turn, as the browser sends it.
type ArchitectureAssistantRequest struct {
	RepositoryID   string `json:"repository_id"`
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	// Draft is the record as the page holds it, saved or not.
	Draft json.RawMessage `json:"draft"`
	// DraftBaseVersion is the saved version the draft started from, 0 when no
	// record exists yet.
	DraftBaseVersion int `json:"draft_base_version"`
	// RuleEventID names a rule event the person asked about, "" for none.
	RuleEventID string `json:"rule_event_id"`
}

// The SSE event names of a turn.
const (
	ArchitectureAssistantEventConversation = "conversation"
	ArchitectureAssistantEventActivity     = "activity"
	ArchitectureAssistantEventDelta        = "delta"
	ArchitectureAssistantEventResult       = "result"
)

// ArchitectureAssistantEvent is something a turn reports; Data is one of the
// types below, by Name.
type ArchitectureAssistantEvent struct {
	Name string
	Data any
}

// ArchitectureAssistantConversation is the first event of a turn.
type ArchitectureAssistantConversation struct {
	ConversationID string `json:"conversation_id"`
}

// ArchitectureAssistantActivity says the assistant used a tool.
type ArchitectureAssistantActivity struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
}

// ArchitectureAssistantDelta is the next piece of the answer text.
type ArchitectureAssistantDelta struct {
	Text string `json:"text"`
}

// ArchitectureAssistantResult is the last event of a successful turn. Proposal
// is nil for a plain answer.
type ArchitectureAssistantResult struct {
	Text     string                         `json:"text"`
	Proposal *ArchitectureAssistantProposal `json:"proposal"`
}

// ArchitectureAssistantProposal is the assistant's proposed changes. Valid is
// false only when the proposal still broke the record's validation after the
// one automatic retry; Errors then say how.
type ArchitectureAssistantProposal struct {
	ID      string                  `json:"id"`
	Summary string                  `json:"summary"`
	Valid   bool                    `json:"valid"`
	Changes []domain.ProposalChange `json:"changes"`
	Errors  []domain.ArchFieldError `json:"errors"`
}

// ArchitectureAssistantAvailability says whether the assistant can run for a
// repository and, when it cannot, why and where to fix it.
type ArchitectureAssistantAvailability struct {
	Available bool
	Reason    string
	FixURL    string
}

// ArchitectureAssistantReadiness tells the pages whether the assistant can
// run, from the readiness state alone: it never starts anything.
type ArchitectureAssistantReadiness interface {
	// Availability says whether a turn could run for the repository now.
	Availability(ctx context.Context, repositoryID string) ArchitectureAssistantAvailability
	// Readiness says whether the harness could run a turn for any repository
	// (Claude signed in, installed, a sandbox driver), for the Models page.
	Readiness(ctx context.Context) ArchitectureAssistantAvailability
}

// ArchitectureAssistant runs the assistant's turns.
type ArchitectureAssistant interface {
	ArchitectureAssistantReadiness
	// Run runs one turn and reports its events in order. A refusal before
	// anything ran (INVALID_INPUT, REPOSITORY_NOT_FOUND, CONVERSATION_NOT_FOUND,
	// CONVERSATION_BUSY, ARCHITECTURE_ASSISTANT_UNAVAILABLE), a failed turn
	// (ARCHITECTURE_ASSISTANT_FAILED) or a cancelled ctx is the returned error.
	// The details of an unavailable assistant carry its fix URL.
	Run(ctx context.Context, req ArchitectureAssistantRequest, emit func(ArchitectureAssistantEvent)) error
	// End forgets a conversation and deletes its transcript. An id that does
	// not exist is not an error.
	End(conversationID string)
}
