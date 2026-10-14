package ports

import (
	"time"

	"operators-mcp/internal/domain"
)

// SessionFilter narrows session listings.
type SessionFilter struct {
	ProjectID string
	AgentID   string
	TicketID  string
	// Statuses matches any of the listed statuses; empty matches all. It is a
	// list because one UI state covers several backend ones — a session the
	// user sees as "running" is `running`, `idle` or `thinking` underneath.
	Statuses []domain.SessionStatus
}

// StoredEvent is one persisted session event.
type StoredEvent struct {
	Seq     int64
	Type    string
	Payload []byte
	At      time.Time
}

// ClaudeTranscripts answers whether an interactive claude conversation can
// still be resumed. The CLI keeps each transcript under the directory the
// conversation ran in, so both the directory and the transcript must exist.
type ClaudeTranscripts interface {
	// CanResume returns nil when conversation claudeSessionID, started in dir,
	// can be resumed; otherwise a StructuredError coded WORKSPACE_MISSING or
	// SESSION_TRANSCRIPT_MISSING.
	CanResume(dir, claudeSessionID string) error
}

// ClaudeTranscriptReader reads a claude conversation transcript, read-only,
// a page at a time.
type ClaudeTranscriptReader interface {
	// ReadTranscript returns a page of conversation claudeSessionID, started
	// in dir. dir only helps find the file: the conversation is read even
	// when dir no longer exists. Errors are StructuredErrors coded
	// SESSION_TRANSCRIPT_MISSING or INVALID_INPUT (a cursor it does not
	// know). q is already validated: a limit in 1–500, at most one cursor.
	ReadTranscript(dir, claudeSessionID string, q TranscriptQuery) (TranscriptPage, error)
}

// Transcript paging bounds.
const (
	TranscriptDefaultLimit = 200
	TranscriptMaxLimit     = 500
)

// TranscriptQuery selects a page: the newest Limit entries with no cursor,
// the Limit entries just older than Before, or up to Limit entries newer
// than After.
type TranscriptQuery struct {
	Before string
	After  string
	Limit  int
}

// TranscriptPage is a page of a transcript, oldest entry first.
type TranscriptPage struct {
	Entries     []TranscriptEntry `json:"entries"`
	FirstCursor string            `json:"first_cursor"`
	LastCursor  string            `json:"last_cursor"`
	HasOlder    bool              `json:"has_older"`
	HasNewer    bool              `json:"has_newer"`
}

// Transcript entry kinds.
const (
	TranscriptUser       = "user"
	TranscriptAssistant  = "assistant"
	TranscriptToolUse    = "tool_use"
	TranscriptToolResult = "tool_result"
	TranscriptSystem     = "system"
)

// TranscriptEntry is one thing the conversation showed.
type TranscriptEntry struct {
	// Cursor is opaque and stable for the transcript file's life.
	Cursor string `json:"cursor"`
	// At is when the CLI recorded it, RFC 3339, or "".
	At   string `json:"at"`
	Kind string `json:"kind"`
	// Text is plain text or markdown as written; for a tool_use, a one-line
	// summary of the call.
	Text string `json:"text"`
	// Tool names the tool of a tool_use or tool_result.
	Tool      string `json:"tool"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated"`
}

// SessionRepository persists sessions and their append-only event log.
type SessionRepository interface {
	Create(s *domain.Session) (*domain.Session, error)
	// Get and List answer a failed read as not found and as no sessions.
	// The session path reads through Find and Query instead, which report it.
	Get(id string) *domain.Session
	List(filter SessionFilter) []*domain.Session
	// Find reads a session: nil, nil when there is none.
	Find(id string) (*domain.Session, error)
	Query(filter SessionFilter) ([]*domain.Session, error)
	// Delete removes the session and its event log.
	Delete(id string) error
	// ActivityByProject answers each project's live session count and newest
	// session activity, for every project that has sessions.
	ActivityByProject() (map[string]SessionActivity, error)
	UpdateStatus(id string, status domain.SessionStatus) error
	UpdateMetrics(id string, costUSD float64, tokens SessionTokens, lastAction string, pendingApprovals int) error
	// UpdateAutoRun records whether the session answers its own permission
	// requests. The gate itself lives in the permission broker; this is the
	// readable copy, so a client does not have to replay the event log to
	// learn the session's current mode.
	UpdateAutoRun(id string, autoRun bool) error
	// UpdateClaudeSessionID records which claude conversation an interactive
	// session resumes into.
	UpdateClaudeSessionID(id, claudeSessionID string) error
	// UpdateRunner records where an interactive session now runs; resuming
	// can move it.
	UpdateRunner(id string, runsOn domain.Runner, host string) error
	// RecordEvent stores one event on the session's log together with what
	// it changes on the session, all or nothing.
	RecordEvent(sessionID string, ev SessionEventRecord) error
	ListEvents(sessionID string, fromSeq int64) ([]StoredEvent, error)
}

// SessionEventRecord is one event of a session's log as stored, with what it
// changes on the session: its metrics when Metrics is set, its status when
// Status is.
type SessionEventRecord struct {
	Seq     int64
	Type    string
	Payload []byte
	Metrics *SessionMetrics
	Status  domain.SessionStatus
}

// SessionMetrics are a session's running totals and latest activity.
type SessionMetrics struct {
	CostUSD          float64
	Tokens           SessionTokens
	LastAction       string
	PendingApprovals int
}

// SessionTokens are a session's token totals, by kind.
type SessionTokens struct {
	Input, Output, CacheCreation, CacheRead int
}

// TokensOf are s's token totals.
func TokensOf(s *domain.Session) SessionTokens {
	return SessionTokens{Input: s.InputTokens, Output: s.OutputTokens, CacheCreation: s.CacheCreationInputTokens, CacheRead: s.CacheReadInputTokens}
}
