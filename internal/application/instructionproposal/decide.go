package instructionproposal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionProposalDecisions = (*Service)(nil)

// FileApplier saves a batch of instruction-file changes in one transaction.
type FileApplier interface {
	ApplyInstructionChanges(ctx context.Context, repositoryID string, changes []domain.InstructionChange) ([]domain.InstructionFile, error)
}

// UseTurns sets where a decision reaches the session that proposed.
func (s *Service) UseTurns(t ports.TurnDelivery) { s.turns = t }

// open is the proposal when it waits on the person, else PROPOSAL_NOT_FOUND or
// PROPOSAL_DECIDED. The caller holds s.mu.
func (s *Service) open(ctx context.Context, proposalID string) (*domain.InstructionProposal, error) {
	p, err := s.proposals.GetInstructionProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, proposalNotFound()
	}
	if p.State != domain.InstructionProposalOpen {
		return nil, &domain.StructuredError{Code: "PROPOSAL_DECIDED", Message: "the proposal is " + p.State + ", not open: it was already decided"}
	}
	return p, nil
}

// decide stores the proposal's new state, announces it, and returns its row.
func (s *Service) decide(ctx context.Context, p *domain.InstructionProposal, state, note string) (*ports.InstructionProposalRow, error) {
	now := time.Now().UTC()
	p.State, p.Note, p.DecidedAt = state, strings.TrimSpace(note), &now
	if err := s.proposals.UpdateInstructionProposal(ctx, p); err != nil {
		return nil, err
	}
	s.announce(ctx, p)
	row := s.row(ctx, p)
	return &row, nil
}

// ApproveInstructionProposal implements ports.InstructionProposalDecisions.
func (s *Service) ApproveInstructionProposal(ctx context.Context, proposalID string) (*ports.InstructionProposalRow, []domain.InstructionFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.open(ctx, proposalID)
	if err != nil {
		return nil, nil, err
	}
	if stale := s.stalePaths(ctx, p); len(stale) > 0 {
		return nil, nil, &domain.DetailedError{
			StructuredError: &domain.StructuredError{Code: "INSTRUCTION_CHANGED", Message: "an instruction file was saved since the draft was read: " + strings.Join(stale, ", ")},
			Details:         map[string]any{"paths": stale},
		}
	}
	changes := make([]domain.InstructionChange, len(p.Files))
	for i, f := range p.Files {
		changes[i] = domain.InstructionChange{Path: f.Path, Kind: "skill", BaseVersion: f.BaseVersion, Content: f.Content, Remove: f.Op == domain.InstructionOpRemove}
	}
	saved, err := s.applier.ApplyInstructionChanges(ctx, p.RepositoryID, changes)
	if err != nil {
		return nil, nil, err
	}
	row, err := s.decide(ctx, p, domain.InstructionProposalApproved, "")
	if err != nil {
		return nil, nil, err
	}
	s.deliver(p, "instruction-approved:"+p.ID, approvedTurn(p, saved))
	return row, saved, nil
}

// stalePaths are the proposal's files whose saved version is no longer the one
// the draft was read at.
func (s *Service) stalePaths(ctx context.Context, p *domain.InstructionProposal) []string {
	var stale []string
	for _, f := range p.Files {
		current := 0
		if saved, err := s.files.GetInstructionFile(ctx, p.RepositoryID, f.Path); err == nil && saved != nil {
			current = saved.Version
		}
		if current != f.BaseVersion {
			stale = append(stale, f.Path)
		}
	}
	return stale
}

// RejectInstructionProposal implements ports.InstructionProposalDecisions.
func (s *Service) RejectInstructionProposal(ctx context.Context, proposalID, note string) (*ports.InstructionProposalRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.open(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	row, err := s.decide(ctx, p, domain.InstructionProposalRejected, note)
	if err != nil {
		return nil, err
	}
	s.deliver(p, "instruction-rejected:"+p.ID, rejectedTurn(p))
	return row, nil
}

// SendBackInstructionProposal implements ports.InstructionProposalDecisions.
func (s *Service) SendBackInstructionProposal(ctx context.Context, proposalID, note string) (*ports.InstructionProposalRow, error) {
	if strings.TrimSpace(note) == "" {
		return nil, invalidInput("note is required: say what the session should change")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.open(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	row, err := s.decide(ctx, p, domain.InstructionProposalSentBack, note)
	if err != nil {
		return nil, err
	}
	s.deliver(p, "instruction-sent-back:"+p.ID, sentBackTurn(p))
	return row, nil
}

// deliver tells the proposing session, when it still runs. One that does not
// reads the outcome later in its transcript, so a refusal is only logged.
func (s *Service) deliver(p *domain.InstructionProposal, key, text string) {
	if s.turns == nil {
		return
	}
	if _, err := s.turns.Deliver(p.SessionID, key, text, nil); err != nil {
		slog.Info("tell a session its instruction proposal was decided", "session", p.SessionID, "proposal", p.ID, "err", err)
	}
}

func approvedTurn(p *domain.InstructionProposal, saved []domain.InstructionFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[instruction files · proposal approved · proposal %s]\n", p.ID)
	for _, f := range p.Files {
		if f.Op == domain.InstructionOpRemove {
			fmt.Fprintf(&b, "%s: removed\n", f.Path)
			continue
		}
		i := slices.IndexFunc(saved, func(s domain.InstructionFile) bool { return s.Path == f.Path })
		if i >= 0 {
			fmt.Fprintf(&b, "%s: version %d\n", f.Path, saved[i].Version)
		}
	}
	b.WriteString("New sessions in the repository receive these files; this session keeps the set it started with.")
	return b.String()
}

func rejectedTurn(p *domain.InstructionProposal) string {
	s := fmt.Sprintf("[instruction files · proposal rejected · proposal %s]", p.ID)
	if p.Note != "" {
		s += "\n" + p.Note
	}
	return s
}

func sentBackTurn(p *domain.InstructionProposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[instruction files · proposal sent back · proposal %s]\n%s\nFiles in the proposal:", p.ID, p.Note)
	for _, f := range p.Files {
		fmt.Fprintf(&b, "\n%s (%s)", f.Path, f.Op)
	}
	b.WriteString("\nThe draft folders are writable again: rework the skills, then call propose_instruction_files again. If the saved files changed meanwhile, rebuild the draft from the current ones.")
	return b.String()
}
