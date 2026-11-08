package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// The interviews of a session (see "Contract: harness server ↔ web UI —
// interviews"). The interviews context owns them; *interviews.Service
// satisfies the driving ports.

// --- Driving ports ---

// InterviewReader reads sessions' interviews, as the seed and the resync
// carry them. A session with none has no entry in the result.
type InterviewReader interface {
	ListInterviews(ctx context.Context, sessionIDs []string) (map[string]*domain.InterviewList, error)
}

// InterviewFeed follows a session's interviews.
type InterviewFeed interface {
	// FollowInterviews streams the session's interview events, numbered by
	// one, until ctx ends. A follower that falls behind is closed and must
	// read the list again.
	FollowInterviews(ctx context.Context, sessionID string) (<-chan domain.InterviewEvent, error)
}

// Interviews is what the HTTP API serves: read the interviews, follow them.
type Interviews interface {
	InterviewReader
	InterviewFeed
}

// InterviewAsker records an interview an agent asked. It is what llmkit's
// Sink calls.
type InterviewAsker interface {
	// Ask stores i, an interview with no state, time or agent name
	// yet, as open on its session and publishes it. A session that is not
	// live refuses it with INTERVIEW_UNAVAILABLE, and nothing is stored.
	Ask(ctx context.Context, i domain.Interview) (*domain.Interview, error)
}

// --- Driven ports ---

// InterviewRepository persists interviews and the number each session's feed
// has reached.
type InterviewRepository interface {
	// Create stores i and answers the session's next feed number, both in
	// one transaction.
	Create(i *domain.Interview) (seq int64, err error)
	// Update replaces the stored interview with i's id and answers the
	// session's next feed number. INTERVIEW_NOT_FOUND when there is none.
	Update(i *domain.Interview) (seq int64, err error)
	// List reads each session's interviews in the contract's order, with
	// the closed ones capped, in a fixed number of queries. Every session
	// asked has an entry: one with no interview is an empty list.
	List(sessionIDs []string) (map[string]*domain.InterviewList, error)
}
