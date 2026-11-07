package domain

import (
	"encoding/json"
	"time"
)

// LiveSessionStatuses are the statuses of a session that is live: the ones an
// Agent board card shows a line for.
var LiveSessionStatuses = []SessionStatus{
	SessionStarting, SessionRunning, SessionThinking, SessionIdle, SessionWaitingApproval, SessionPaused,
}

// IsLive reports whether a session in status s is live (not done, failed or
// stopped).
func (s SessionStatus) IsLive() bool {
	for _, live := range LiveSessionStatuses {
		if s == live {
			return true
		}
	}
	return false
}

// NeedsPerson reports whether a live session in status s waits on the person:
// it is the person's turn or it asks for approval.
func (s SessionStatus) NeedsPerson() bool {
	return s == SessionIdle || s == SessionWaitingApproval
}

// BoardSessionLine is one live session on an Agent board card.
type BoardSessionLine struct {
	SessionID   string        `json:"session_id"`
	AgentName   string        `json:"agent_name"`
	Role        string        `json:"role"` // architect, delegate or peer
	Status      SessionStatus `json:"status"`
	StatusSince time.Time     `json:"status_since"`
	// WaitingFor is the pending approval's one-line ask (at most 120
	// characters, cut with "…"); nil when the session is not waiting for
	// approval or the server cannot say.
	WaitingFor *string `json:"waiting_for"`
	Mode       string  `json:"mode"`
	Model      string  `json:"model"`
}

// BoardCard is one task with at least one live session.
type BoardCard struct {
	TaskID       string  `json:"task_id"`
	ProjectID    string  `json:"project_id"`
	TaskTitle    string  `json:"task_title"`
	TaskStatus   string  `json:"task_status"`
	FeatureID    *string `json:"feature_id"`
	FeatureTitle *string `json:"feature_title"`
	// NeedsYou is true exactly when a live session is idle or waiting for
	// approval. The server decides it; clients never derive it.
	NeedsYou           bool               `json:"needs_you"`
	NeedsYouSince      *time.Time         `json:"needs_you_since"`
	LastStatusChangeAt time.Time          `json:"last_status_change_at"`
	Sessions           []BoardSessionLine `json:"sessions"` // most recent status_since first
}

// BoardProject is a project section of the seed.
type BoardProject struct {
	ProjectID   string      `json:"project_id"`
	ProjectName string      `json:"project_name"`
	Cards       []BoardCard `json:"cards"`
}

// BoardSeed is the Agent board as of Seq: what GET / renders and
// GET /api/agent_board answers.
type BoardSeed struct {
	Seq           int64          `json:"seq"`
	GeneratedAt   time.Time      `json:"generated_at"`
	ProjectCount  int            `json:"project_count"`
	QuietProjects int            `json:"quiet_projects"`
	Projects      []BoardProject `json:"projects"`
}

// Kinds of live session feed event.
const (
	BoardSessionLive    = "session_live"
	BoardSessionEnded   = "session_ended"
	BoardTaskChanged    = "task_changed"
	BoardProjectChanged = "project_changed"
)

// BoardEvent is one event of the live session feed. Which fields are set
// depends on Kind (see "Contract: harness server ↔ web UI — the agent board
// seed and the live session feed").
type BoardEvent struct {
	Seq         int64             `json:"seq"`
	Kind        string            `json:"kind"`
	ProjectID   string            `json:"project_id"`
	ProjectName string            `json:"project_name,omitempty"`
	TaskID      string            `json:"task_id,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	Ended       string            `json:"ended,omitempty"` // done, failed, stopped or deleted
	Session     *BoardSessionLine `json:"session,omitempty"`
	// Card is the whole card after the change; nil (JSON null) when the task
	// has no live session left. Not sent on project_changed.
	Card *BoardCard `json:"card,omitempty"`
}

// MarshalJSON sends a session_ended event's card as an explicit null when the
// task has no live session left; every other kind leaves it out when unset.
func (e BoardEvent) MarshalJSON() ([]byte, error) {
	type plain BoardEvent
	if e.Kind == BoardSessionEnded {
		return json.Marshal(struct {
			plain
			Card *BoardCard `json:"card"`
		}{plain(e), e.Card})
	}
	return json.Marshal(plain(e))
}
