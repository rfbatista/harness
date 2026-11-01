package domain

import "time"

// The states of a design-system proposal (ADR 0055). A proposal is drafting
// while its design session works on the draft, open once it is submitted and
// waits on the person, then decided or withdrawn.
const (
	ProposalDrafting       = "drafting"
	ProposalOpen           = "open"
	ProposalApproved       = "approved"
	ProposalPartlyApproved = "partly_approved"
	ProposalRejected       = "rejected"
	ProposalWithdrawn      = "withdrawn"
)

// DesignSourceProposal is the source of a version an approved proposal wrote.
const DesignSourceProposal = "proposal"

// DesignProposal is one design session's draft of a design system and, once
// submitted, its proposal: the draft is compared with the version it started
// from (BaseVersion) to find the items the person decides on.
type DesignProposal struct {
	ID             string
	DesignSystemID string
	SessionID      string
	State          string
	// BaseVersion is the design system version the draft started from; 0 for
	// a design system with no version yet.
	BaseVersion int
	Summary     string
	// Draft is the session's working copy of the record.
	Draft DesignSystemRecord
	// DecidedVersion is the version an approval wrote, 0 for none.
	DecidedVersion int
	// Rejected is how many items a decision rejected, for the decided turn.
	Approved, Rejected int
	SubmittedAt        time.Time
	DecidedAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Active says the proposal still belongs to its session's draft: the session
// works on it, or it waits on the person.
func (p *DesignProposal) Active() bool {
	return p.State == ProposalDrafting || p.State == ProposalOpen
}
