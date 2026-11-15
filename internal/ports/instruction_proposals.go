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

// InstructionProposalRow is a proposal with the names the list shows: its
// task's title and the agent and mode of the session that submitted it.
type InstructionProposalRow struct {
	*domain.InstructionProposal
	TaskTitle string
	Agent     string
	Mode      string
}

// InstructionProposalFileView is a file of a proposal with the version of the
// saved instruction file now, and whether that moved since the draft was read.
type InstructionProposalFileView struct {
	domain.InstructionProposalFile
	CurrentVersion int
	Stale          bool
}

// InstructionProposals is what the web UI does with a repository's
// instruction-file proposals (ADR 0092).
type InstructionProposals interface {
	// ListInstructionProposals answers the repository's proposals, newest
	// first, those in state when it is set, and how many are open.
	ListInstructionProposals(ctx context.Context, repositoryID, state string) ([]InstructionProposalRow, int, error)
	GetInstructionProposal(ctx context.Context, proposalID string) (*InstructionProposalRow, []InstructionProposalFileView, error)
}
