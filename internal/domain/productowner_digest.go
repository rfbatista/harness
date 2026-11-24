package domain

import "time"

// A project's daily digest (ADR 0113) is a dated record of what changed in the
// project since the last one: the items the server collected, and the summary
// the product owner wrote of them.

// Kinds of digest item.
const (
	DigestItemTaskMoved       = "task_moved"
	DigestItemSessionEnded    = "session_ended"
	DigestItemSessionFailed   = "session_failed"
	DigestItemWaitingOnPerson = "waiting_on_person"
)

// ProductOwnerDigestPage is how many digests one read answers.
const ProductOwnerDigestPage = 30

// ProductOwnerDigestItem is one thing that changed: a task that moved (From
// and To), a session that ended or failed, or something that waits on the
// person. URL is its page on the web UI.
type ProductOwnerDigestItem struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

// ProductOwnerDigest is one digest of a project. Date is the local day it was
// recorded on, "2006-01-02".
type ProductOwnerDigest struct {
	ID          string                   `json:"digest_id"`
	ProjectID   string                   `json:"-"`
	Date        string                   `json:"date"`
	PeriodStart time.Time                `json:"period_start"`
	PeriodEnd   time.Time                `json:"period_end"`
	Summary     string                   `json:"summary"`
	Items       []ProductOwnerDigestItem `json:"items"`
}

// ProductOwnerDigestState is where a project's digests stand: the start of the
// period the next digest covers, and the last day the scheduler looked.
type ProductOwnerDigestState struct {
	ProjectID string
	// SinceAt is when the digest time was first set, or where the last digest
	// ended.
	SinceAt time.Time
	// CheckedDate is the last local day the collection ran, empty before the
	// first one. A quiet day is checked as a busy one is.
	CheckedDate string
}
