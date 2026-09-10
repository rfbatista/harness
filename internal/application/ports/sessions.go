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

// SessionRepository persists sessions and their append-only event log.
type SessionRepository interface {
	Create(s *domain.Session) (*domain.Session, error)
	Get(id string) *domain.Session
	List(filter SessionFilter) []*domain.Session
	// Delete removes the session and its event log.
	Delete(id string) error
	UpdateStatus(id string, status domain.SessionStatus) error
	UpdateMetrics(id string, costUSD float64, inputTokens, outputTokens int, lastAction string, pendingApprovals int) error
	// UpdateAutoRun records whether the session answers its own permission
	// requests. The gate itself lives in the permission broker; this is the
	// readable copy, so a client does not have to replay the event log to
	// learn the session's current mode.
	UpdateAutoRun(id string, autoRun bool) error
	AppendEvent(sessionID string, seq int64, typ string, payload []byte) error
	ListEvents(sessionID string, fromSeq int64) []StoredEvent
}
