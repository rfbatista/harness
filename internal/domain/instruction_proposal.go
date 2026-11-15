package domain

import "time"

// The states of an instruction-file proposal (ADR 0092).
const (
	InstructionProposalOpen     = "open"
	InstructionProposalApproved = "approved"
	InstructionProposalRejected = "rejected"
	InstructionProposalSentBack = "sent_back"
)

// The operations a proposal makes on an instruction file.
const (
	InstructionOpCreate = "create"
	InstructionOpChange = "change"
	InstructionOpRemove = "remove"
)

// InstructionProposalFile is one file of a proposal: what the draft would
// make of an instruction file, and the version of it the draft was read at.
type InstructionProposalFile struct {
	Path        string
	Op          string
	BaseVersion int // 0 for a create
	// BaseContent is the instruction file at BaseVersion, empty for a create;
	// Content is the proposed content, empty for a remove.
	BaseContent string
	Content     string
}

// InstructionProposal is a session's draft skills of one repository, waiting
// on the person (ADR 0092).
type InstructionProposal struct {
	ID           string
	RepositoryID string
	ProjectID    string
	TaskID       string
	SessionID    string
	Summary      string
	Skills       []string
	State        string
	Note         string
	Files        []InstructionProposalFile
	CreatedAt    time.Time
	DecidedAt    *time.Time
}
