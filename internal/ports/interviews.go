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

// InterviewResponder closes an interview with the person's answer and sends
// the agent its next turn. Both are idempotent per interview id: a repeat of
// the operation that already succeeded returns the stored interview and
// delivers nothing.
type InterviewResponder interface {
	// Submit sends the answers, one per question. Refusals: INTERVIEW_NOT_FOUND;
	// INTERVIEW_NOT_OPEN (dismissed or expired), INTERVIEW_EXPIRED (the session
	// ended; the interview is now expired), each carrying the current
	// interview; INTERVIEW_ANSWERS_INVALID.
	Submit(ctx context.Context, id string, answers []domain.InterviewAnswer) (*domain.Interview, error)
	// Dismiss closes the interview without answers, with an optional note.
	// Refusals as Submit's, except INTERVIEW_NOT_OPEN is for sent or expired.
	Dismiss(ctx context.Context, id, note string) (*domain.Interview, error)
}

// Interviews is what the HTTP API serves: read the interviews, follow them,
// answer them.
type Interviews interface {
	InterviewReader
	InterviewFeed
	InterviewResponder
}

// InterviewTurns is the provider-owned side of answering: the rules the
// answers must meet and the text of the turn the agent receives. The
// harness hands the text on as it is.
type InterviewTurns interface {
	// ValidateAnswers reports the first rule the answers break for iv.
	ValidateAnswers(iv domain.Interview, answers []domain.InterviewAnswer) error
	RenderAnswers(iv domain.Interview, answers []domain.InterviewAnswer) string
	RenderDismissed(iv domain.Interview, note string) string
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
	// Get reads an interview. INTERVIEW_NOT_FOUND when there is none.
	Get(id string) (*domain.Interview, error)
	// Close replaces the stored interview with i's id by i, which leaves open,
	// only while the stored one is still open: compare-and-set in one
	// statement. It answers whether it won; a loser changed nothing and took
	// no feed number. INTERVIEW_NOT_FOUND when there is none.
	Close(i *domain.Interview) (won bool, seq int64, err error)
	// List reads each session's interviews in the contract's order, with
	// the closed ones capped, in a fixed number of queries. Every session
	// asked has an entry: one with no interview is an empty list.
	List(sessionIDs []string) (map[string]*domain.InterviewList, error)
}
