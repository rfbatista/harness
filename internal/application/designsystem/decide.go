package designsystem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// UseTurns sets where a decision reaches the design session that proposed.
func (s *Service) UseTurns(t ports.TurnDelivery) { s.turns = t }

// DecideDesignProposal implements ports.DesignProposals: the person marks
// items approved or rejected, in one call or over several. The approved items
// of a call are applied on top of the current version as one version
// attributed to the proposal; items left out stay pending and keep the
// proposal open. Once none is pending the proposal is approved, rejected or
// partly approved.
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
	approving, marked, err := markedItems(items, decisions)
	if err != nil {
		return nil, err
	}
	if err := confirmed(approving, confirmOverwrite); err != nil {
		return nil, err
	}
	before := *p
	before.Decisions = maps.Clone(p.Decisions)
	if p.Decisions == nil {
		p.Decisions = map[string]string{}
	}
	for itemID, d := range marked {
		p.Decisions[itemID] = d
	}
	p.State, p.Approved, p.Rejected = tally(items, p.Decisions)
	if p.State != domain.ProposalOpen {
		p.DecidedAt = s.now()
	}
	// The decisions are stored before the version is written, so a version is
	// never written for a proposal that still waits on them; a refused write
	// puts the proposal back.
	if err := s.proposals.Update(ctx, p); err != nil {
		return nil, err
	}
	out := &ports.DesignDecision{State: p.State}
	if len(approving) > 0 {
		v, err := s.applyApproved(ctx, id, cur, p, approving)
		if err != nil {
			*p = before
			if undo := s.proposals.Update(ctx, p); undo != nil {
				slog.Error("put a proposal back after a refused apply", "proposal", p.ID, "err", undo)
			}
			return nil, blameItems(err, cur, approving)
		}
		p.DecidedVersion = v.Version
		out.Version = &v.Version
		if err := s.proposals.Update(ctx, p); err != nil {
			return nil, err
		}
	}
	s.announceProposal(id.ProjectID, p)
	if p.State != domain.ProposalOpen {
		s.deliverDecision(p, rejectedItems(items, p.Decisions))
	}
	return out, nil
}

// markedItems reads the person's decisions: every one names a pending item of
// the proposal and says approve or reject. It answers the items to apply and
// each marked item's new decision.
func markedItems(items []domain.DesignItem, decisions []ports.ItemDecision) (approving []domain.DesignItem, marked map[string]string, err error) {
	bad := func(format string, a ...any) error {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: fmt.Sprintf(format, a...)}
	}
	if len(decisions) == 0 {
		return nil, nil, bad("decisions is required: approve or reject at least one item")
	}
	marked = map[string]string{}
	for _, d := range decisions {
		var value string
		switch d.Decision {
		case "approve":
			value = domain.DecisionApproved
		case "reject":
			value = domain.DecisionRejected
		default:
			return nil, nil, bad("decision %q for item %s is neither approve nor reject", d.Decision, d.ItemID)
		}
		i := slices.IndexFunc(items, func(it domain.DesignItem) bool { return it.ID == d.ItemID })
		if i < 0 {
			return nil, nil, bad("the proposal has no item %s", d.ItemID)
		}
		if items[i].Decision != domain.DecisionPending {
			return nil, nil, bad("item %s is already %s", items[i].Key, items[i].Decision)
		}
		if _, twice := marked[d.ItemID]; twice {
			return nil, nil, bad("item %s is decided twice", items[i].Key)
		}
		marked[d.ItemID] = value
	}
	for _, it := range items {
		if marked[it.ID] == domain.DecisionApproved {
			approving = append(approving, it)
		}
	}
	return approving, marked, nil
}

// tally is the state the decisions put the proposal in, with how many items
// are approved and rejected: open while one is pending, else approved,
// rejected or partly approved.
func tally(items []domain.DesignItem, decisions map[string]string) (state string, approved, rejected int) {
	pending := 0
	for _, it := range items {
		switch decisions[it.ID] {
		case domain.DecisionApproved:
			approved++
		case domain.DecisionRejected:
			rejected++
		default:
			pending++
		}
	}
	switch {
	case pending > 0:
		return domain.ProposalOpen, approved, rejected
	case rejected == 0:
		return domain.ProposalApproved, approved, rejected
	case approved == 0:
		return domain.ProposalRejected, approved, rejected
	}
	return domain.ProposalPartlyApproved, approved, rejected
}

func rejectedItems(items []domain.DesignItem, decisions map[string]string) []domain.DesignItem {
	var out []domain.DesignItem
	for _, it := range items {
		if decisions[it.ID] == domain.DecisionRejected {
			out = append(out, it)
		}
	}
	return out
}

// blameItems adds to a refused apply the keys of the approved items the
// design system's validation names, so the person can reject or fix them.
func blameItems(err error, cur *domain.DesignSystemVersion, approving []domain.DesignItem) error {
	var detailed *domain.DetailedError
	if !errors.As(err, &detailed) || detailed.Code != "DESIGN_SYSTEM_INVALID" {
		return err
	}
	details, _ := detailed.Details.(map[string]any)
	problems, _ := details["errors"].([]domain.DesignFieldError)
	var current domain.DesignSystemRecord
	if cur != nil {
		current = cur.Record
	}
	record := domain.ApplyDesignItems(current, approving)
	var keys []string
	name := func(key string) {
		for _, it := range approving {
			if it.Key == key && !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	for _, pr := range problems {
		parts := strings.Split(strings.TrimPrefix(pr.Path, "/"), "/")
		var n int
		if len(parts) >= 2 {
			n, _ = strconv.Atoi(parts[1])
		}
		switch {
		case parts[0] == "components" && len(parts) >= 2 && n < len(record.Components):
			name(record.Components[n].Slug)
		case parts[0] == "tokens" && len(parts) >= 2 && n < len(record.Tokens):
			name(record.Tokens[n].Key())
		case pr.Code == domain.DesignTokenInUse:
			// "token <key> is used by <slug>, <slug>"
			fields := strings.Fields(strings.NewReplacer(",", " ").Replace(pr.Message))
			if len(fields) > 1 {
				name(fields[1])
			}
			if _, users, ok := strings.Cut(pr.Message, " is used by "); ok {
				for _, slug := range strings.Split(users, ", ") {
					name(slug)
				}
			}
		}
	}
	if keys == nil {
		keys = []string{}
	}
	out := *detailed
	withKeys := maps.Clone(details)
	withKeys["item_keys"] = keys
	out.Details = withKeys
	return &out
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
