package instructionproposal

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionProposals = (*Service)(nil)

// UseTasks sets where a proposal's task title is read.
func (s *Service) UseTasks(t ports.TicketReader) { s.tasks = t }

// UseAgents sets where the agent of a proposal's session is named.
func (s *Service) UseAgents(a ports.AgentLister) { s.agents = a }

func proposalNotFound() error {
	return &domain.StructuredError{Code: "PROPOSAL_NOT_FOUND", Message: "proposal not found"}
}

// row names the task and the session of a proposal.
func (s *Service) row(ctx context.Context, p *domain.InstructionProposal) ports.InstructionProposalRow {
	row := ports.InstructionProposalRow{InstructionProposal: p}
	if s.tasks != nil {
		if tk, err := s.tasks.GetTicket(ctx, p.TaskID); err == nil && tk != nil {
			row.TaskTitle = tk.Title
		}
	}
	if s.sessions != nil {
		if sess := s.sessions.Get(p.SessionID); sess != nil {
			row.Mode = string(sess.Mode)
			row.Agent = s.agentName(ctx, sess.AgentID)
		}
	}
	return row
}

// agentName is the agent's name, "" for none or one that is gone.
func (s *Service) agentName(ctx context.Context, id string) string {
	if s.agents == nil || id == "" {
		return ""
	}
	all, err := s.agents.ListAgents(ctx)
	if err != nil {
		return ""
	}
	for _, a := range all {
		if a.ID == id {
			return a.Name
		}
	}
	return ""
}

// ListInstructionProposals implements ports.InstructionProposals.
func (s *Service) ListInstructionProposals(ctx context.Context, repositoryID, state string) ([]ports.InstructionProposalRow, int, error) {
	if _, err := s.repos.GetRepository(ctx, repositoryID); err != nil {
		return nil, 0, err
	}
	list, err := s.proposals.ListInstructionProposalsByRepository(ctx, repositoryID)
	if err != nil {
		return nil, 0, err
	}
	rows := []ports.InstructionProposalRow{}
	open := 0
	for _, p := range list {
		if p.State == domain.InstructionProposalOpen {
			open++
		}
		if state == "" || p.State == state {
			rows = append(rows, s.row(ctx, p))
		}
	}
	return rows, open, nil
}

// GetInstructionProposal implements ports.InstructionProposals.
func (s *Service) GetInstructionProposal(ctx context.Context, proposalID string) (*ports.InstructionProposalRow, []ports.InstructionProposalFileView, error) {
	p, err := s.proposals.GetInstructionProposal(ctx, proposalID)
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		return nil, nil, proposalNotFound()
	}
	files := make([]ports.InstructionProposalFileView, len(p.Files))
	for i, f := range p.Files {
		current := 0
		if saved, err := s.files.GetInstructionFile(ctx, p.RepositoryID, f.Path); err == nil && saved != nil {
			current = saved.Version
		}
		files[i] = ports.InstructionProposalFileView{InstructionProposalFile: f, CurrentVersion: current, Stale: current != f.BaseVersion}
	}
	row := s.row(ctx, p)
	return &row, files, nil
}
