package designsystem

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

// UseTurns sets where a decision reaches the design session that proposed.
func (s *Service) UseTurns(t ports.TurnDelivery) { s.turns = t }

// DecideDesignProposal implements ports.DesignProposals: every item approved
// or every item rejected. Approving applies the items on top of the current
// version as one version attributed to the proposal; rejecting changes
// nothing and keeps the proposal as rejected.
func (s *Service) DecideDesignProposal(ctx context.Context, proposalID string, decisions []ports.ItemDecision, confirmOverwrite []string) (*ports.DesignDecision, error) {
	if s.proposals == nil {
		return nil, proposalNotFound()
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	p, err := s.proposals.Get(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, proposalNotFound()
	}
	if p.State != domain.ProposalOpen {
		return nil, &domain.StructuredError{Code: "PROPOSAL_NOT_OPEN", Message: "the proposal is " + p.State + ", not open: nothing to decide"}
	}
	id, cur, err := s.current(ctx, p.DesignSystemID)
	if err != nil {
		return nil, err
	}
	items, err := s.items(ctx, p, cur)
	if err != nil {
		return nil, err
	}
	approve, err := wholeDecision(items, decisions)
	if err != nil {
		return nil, err
	}
	out := &ports.DesignDecision{}
	var rejected []domain.DesignItem
	if approve {
		if err := confirmed(items, confirmOverwrite); err != nil {
			return nil, err
		}
		// The proposal leaves open first, so a version is never written for a
		// proposal that still waits; a refused write puts it back.
		p.State, p.Approved, p.DecidedAt = domain.ProposalApproved, len(items), s.now()
		if err := s.proposals.Update(ctx, p); err != nil {
			return nil, err
		}
		v, err := s.applyApproved(ctx, id, cur, p, items)
		if err != nil {
			p.State, p.Approved, p.DecidedAt = domain.ProposalOpen, 0, time.Time{}
			if undo := s.proposals.Update(ctx, p); undo != nil {
				slog.Error("put a proposal back to open after a refused approval", "proposal", p.ID, "err", undo)
			}
			return nil, err
		}
		p.DecidedVersion = v.Version
		out.Version = &v.Version
	} else {
		p.State, p.Rejected, p.DecidedAt, rejected = domain.ProposalRejected, len(items), s.now(), items
	}
	if err := s.proposals.Update(ctx, p); err != nil {
		return nil, err
	}
	out.State = p.State
	s.announceProposal(id.ProjectID, p)
	s.deliverDecision(p, rejected)
	return out, nil
}

// wholeDecision reads the decisions as one for every item: this slice takes
// all approved or all rejected, never some.
func wholeDecision(items []domain.DesignItem, decisions []ports.ItemDecision) (approve bool, err error) {
	bad := func(format string, a ...any) error {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: fmt.Sprintf(format, a...)}
	}
	if len(decisions) == 0 {
		return false, bad("decisions is required: approve or reject every item")
	}
	byItem := map[string]string{}
	for _, d := range decisions {
		if d.Decision != "approve" && d.Decision != "reject" {
			return false, bad("decision %q for item %s is neither approve nor reject", d.Decision, d.ItemID)
		}
		if !slices.ContainsFunc(items, func(it domain.DesignItem) bool { return it.ID == d.ItemID }) {
			return false, bad("the proposal has no item %s", d.ItemID)
		}
		byItem[d.ItemID] = d.Decision
	}
	var approved, rejected int
	for _, it := range items {
		switch byItem[it.ID] {
		case "approve":
			approved++
		case "reject":
			rejected++
		}
	}
	if (approved != len(items) && rejected != len(items)) || len(items) == 0 {
		return false, bad("decide every item the same way: this server takes approve all or reject all, not some items")
	}
	return approved == len(items), nil
}

// confirmed refuses an approval that overwrites a conflicting item the person
// did not confirm, naming them.
func confirmed(items []domain.DesignItem, confirm []string) error {
	var missing []string
	for _, it := range items {
		if it.Conflicting && !slices.Contains(confirm, it.ID) {
			missing = append(missing, it.ID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "CONFLICT_NOT_CONFIRMED", Message: "the design system changed since the proposal's base: confirm overwriting each conflicting item"},
		Details:         map[string]any{"item_ids": missing},
	}
}

// applyApproved writes the items on top of the current version as the next
// one, through the validation of a save.
func (s *Service) applyApproved(ctx context.Context, id *domain.DesignSystemIdentity, cur *domain.DesignSystemVersion, p *domain.DesignProposal, items []domain.DesignItem) (*domain.DesignSystemVersion, error) {
	var current domain.DesignSystemRecord
	base := 0
	if cur != nil {
		current, base = cur.Record, cur.Version
	}
	record := domain.ApplyDesignItems(current, items)
	message := p.Summary
	return s.writeFrom(ctx, id, cur, base, record, message, origin{by: "proposal " + p.ID, source: domain.DesignSourceProposal, proposalID: p.ID})
}

// deliverDecision tells the design session what the person decided, when it
// still runs. Its draft is the current version again: the proposal is no
// longer active.
func (s *Service) deliverDecision(p *domain.DesignProposal, rejected []domain.DesignItem) {
	if s.turns == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[design system · proposal decided · proposal %s]\napproved %d, rejected %d", p.ID, p.Approved, p.Rejected)
	if p.DecidedVersion > 0 {
		fmt.Fprintf(&b, "\nThe design system is now at version %d.", p.DecidedVersion)
	} else {
		b.WriteString("\nThe design system did not change.")
	}
	for _, it := range rejected {
		fmt.Fprintf(&b, "\nrejected: %s", it.Key)
	}
	b.WriteString("\nYour draft now starts again from the current version; you may start a new proposal.")
	if _, err := s.turns.Deliver(p.SessionID, "design-decided:"+p.ID, b.String(), nil); err != nil {
		slog.Info("tell a design session its proposal was decided", "session", p.SessionID, "proposal", p.ID, "err", err)
	}
}

// WithdrawDesignProposal implements ports.DesignProposals: the person drops a
// proposal whose session ended.
func (s *Service) WithdrawDesignProposal(ctx context.Context, proposalID string) (string, error) {
	if s.proposals == nil {
		return "", proposalNotFound()
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	p, err := s.proposals.Get(ctx, proposalID)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", proposalNotFound()
	}
	if !p.Active() {
		return "", &domain.StructuredError{Code: "PROPOSAL_NOT_OPEN", Message: "the proposal is " + p.State + ": nothing to withdraw"}
	}
	if s.sessionStore != nil {
		if sess := s.sessionStore.Get(p.SessionID); sess != nil && !sess.Status.IsTerminal() {
			return "", &domain.StructuredError{Code: "SESSION_RUNNING", Message: "the proposal's design session still runs: withdraw it once the session has ended"}
		}
	}
	id, err := s.identity(ctx, p.DesignSystemID)
	if err != nil {
		return "", err
	}
	p.State, p.DecidedAt = domain.ProposalWithdrawn, s.now()
	if err := s.proposals.Update(ctx, p); err != nil {
		return "", err
	}
	s.announceProposal(id.ProjectID, p)
	return p.State, nil
}
