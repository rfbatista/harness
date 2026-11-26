package ports

import "context"

// CriticReviewStore remembers which diffs a critic was launched for, so no
// diff is reviewed twice (driven; sqlite implements it). A diff is the author
// session's base and head commits.
type CriticReviewStore interface {
	// Seen says a critic was launched for this diff of the session.
	Seen(ctx context.Context, sessionID, base, head string) (bool, error)
	// Claim records the diff as reviewed by the critic run for gateRunID and
	// says whether this call recorded it: false means it was recorded before,
	// so two runs finishing together launch one critic.
	Claim(ctx context.Context, sessionID, base, head, gateRunID string) (bool, error)
	// Release forgets a claim whose critic could not be started, so the diff
	// is reviewed by a later run.
	Release(ctx context.Context, sessionID, base, head string) error
}
