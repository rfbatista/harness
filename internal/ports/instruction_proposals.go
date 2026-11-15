package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// InstructionProposalStore keeps instruction-file proposals and their files
// (driven).
type InstructionProposalStore interface {
	CreateInstructionProposal(ctx context.Context, p *domain.InstructionProposal) error
	// GetInstructionProposal answers nil when there is no such proposal.
	GetInstructionProposal(ctx context.Context, id string) (*domain.InstructionProposal, error)
	// ListInstructionProposalsByRepository and ...ByTask answer the
	// proposals, newest first, with their files.
	ListInstructionProposalsByRepository(ctx context.Context, repositoryID string) ([]*domain.InstructionProposal, error)
	ListInstructionProposalsByTask(ctx context.Context, taskID string) ([]*domain.InstructionProposal, error)
	// UpdateInstructionProposal stores the proposal's state, note and
	// decision time.
	UpdateInstructionProposal(ctx context.Context, p *domain.InstructionProposal) error
}

// InstructionSubmission is what submitting a proposal answers the session.
type InstructionSubmission struct {
	ProposalID string
	Files      int
	Created    int
	Changed    int
	Removed    int
}

// InstructionProposalSubmitter is what the session tool propose_instruction_files
// calls (ADR 0092).
type InstructionProposalSubmitter interface {
	SubmitInstructionProposal(ctx context.Context, sessionID, repository string, skills []string, summary string) (*InstructionSubmission, error)
}

// InstructionProposalChange is a proposal that reached a state, with the
// number of open proposals its repository has now. ProjectID routes it and is
// not on the wire.
type InstructionProposalChange struct {
	RepositoryID string `json:"repository_id"`
	ProposalID   string `json:"proposal_id"`
	State        string `json:"state"`
	OpenCount    int    `json:"open_count"`
	ProjectID    string `json:"-"`
}
