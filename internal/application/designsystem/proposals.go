package designsystem

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignProposals = (*Service)(nil)
var _ ports.DesignProposalSubmitter = (*Service)(nil)

// UseNotifier sets where an open proposal is announced to the person's inbox.
func (s *Service) UseNotifier(n ports.Notifier) { s.notifier = n }

func (s *Service) announceProposal(projectID string, p *domain.DesignProposal) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{DesignSystemProposal: &ports.DesignSystemProposalChange{
		ProjectID: projectID, DesignSystemID: p.DesignSystemID, ProposalID: p.ID, State: p.State,
	}})
}

// UseAgents sets where an agent's name is read for the proposals' rows.
func (s *Service) UseAgents(a interface {
	GetAgent(ctx context.Context, id string) (*domain.Agent, error)
}) {
	s.agents = a
}

// agentName is the agent's name, "" for none or one that is gone.
func (s *Service) agentName(id string) string {
	if s.agents == nil || id == "" {
		return ""
	}
	a, err := s.agents.GetAgent(context.Background(), id)
	if err != nil || a == nil {
		return ""
	}
	return a.Name
}

// proposalNotFound is PROPOSAL_NOT_FOUND.
func proposalNotFound() error {
	return &domain.StructuredError{Code: "PROPOSAL_NOT_FOUND", Message: "proposal not found"}
}

// SubmitDesignProposal implements ports.DesignProposalSubmitter: the design
// session's draft becomes its open proposal.
func (s *Service) SubmitDesignProposal(ctx context.Context, sessionID, summary string) (*ports.DesignSubmission, error) {
	if !s.IsDesignSession(sessionID) {
		return nil, &domain.StructuredError{Code: "NOT_A_DESIGN_SESSION", Message: "submit_design_system_proposal is only for a design session"}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "summary is required: say in a line what the proposal changes and why"}
	}
	sess := s.sessionStore.Get(sessionID)
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	id, cur, err := s.current(ctx, sess.DesignSystemID)
	if err != nil {
		return nil, err
	}
	p, err := s.proposals.ActiveBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if p != nil && p.State == domain.ProposalOpen {
		return nil, &domain.StructuredError{Code: "PROPOSAL_SUBMITTED", Message: "your proposal already waits on the person; it returns to you if they send items back"}
	}
	if p == nil {
		return nil, &domain.StructuredError{Code: "PROPOSAL_EMPTY", Message: "your draft equals its base: change tokens.json, DESIGN.md or a component first"}
	}
	items, err := s.items(ctx, p, cur)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, &domain.StructuredError{Code: "PROPOSAL_EMPTY", Message: "your draft equals its base: change tokens.json, DESIGN.md or a component first"}
	}
	p.State, p.Summary, p.SubmittedAt = domain.ProposalOpen, summary, s.now()
	if err := s.proposals.Update(ctx, p); err != nil {
		return nil, err
	}
	s.announceProposal(id.ProjectID, p)
	s.notifyOpen(ctx, id, sess, p)
	conflicting := 0
	for _, it := range items {
		if it.Conflicting {
			conflicting++
		}
	}
	return &ports.DesignSubmission{ProposalID: p.ID, Items: len(items), Conflicting: conflicting}, nil
}

// notifyOpen posts the inbox notification of a proposal that reached open:
// level action, attributed to the design session, linking the Proposals tab
// with the proposal selected. A failure is logged: the proposal is open.
func (s *Service) notifyOpen(ctx context.Context, id *domain.DesignSystemIdentity, sess *domain.Session, p *domain.DesignProposal) {
	if s.notifier == nil {
		return
	}
	_, err := s.notifier.Notify(ctx, ports.NotifyRequest{
		SessionID: sess.ID, ProjectID: id.ProjectID,
		Title: "Design proposal for " + id.Name,
		Body:  p.Summary,
		Level: domain.NotificationAction,
		Link:  "/projects/" + url.PathEscape(id.ProjectID) + "/design-systems/" + url.PathEscape(id.Slug) + "?tab=proposals&proposal=" + url.QueryEscape(p.ID),
	})
	if err != nil {
		slog.Warn("notify an open design proposal", "proposal", p.ID, "err", err)
	}
}

// items are the changes the proposal's draft makes to its base, with the
// conflicts against the design system's current version cur.
func (s *Service) items(ctx context.Context, p *domain.DesignProposal, cur *domain.DesignSystemVersion) ([]domain.DesignItem, error) {
	base, err := s.baseRecord(ctx, p.DesignSystemID, p.BaseVersion)
	if err != nil {
		return nil, err
	}
	var current domain.DesignSystemRecord
	if cur != nil {
		current = cur.Record
	}
	current.Normalize()
	return domain.DeriveDesignItems(base, p.Draft, current), nil
}

// ListDesignProposals implements ports.DesignProposals.
func (s *Service) ListDesignProposals(ctx context.Context, designSystemID, state string) ([]*ports.DesignProposalRow, error) {
	_, cur, err := s.current(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	if s.proposals == nil {
		return []*ports.DesignProposalRow{}, nil
	}
	list, err := s.proposals.ListByDesignSystem(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	out := []*ports.DesignProposalRow{}
	for _, p := range list {
		if state != "" && p.State != state {
			continue
		}
		items, err := s.items(ctx, p, cur)
		if err != nil {
			return nil, err
		}
		if p.State == domain.ProposalDrafting && len(items) == 0 {
			continue // nothing to decide on yet: a draft the session has not changed
		}
		out = append(out, s.row(p, items))
	}
	return out, nil
}

func (s *Service) row(p *domain.DesignProposal, items []domain.DesignItem) *ports.DesignProposalRow {
	row := &ports.DesignProposalRow{
		ID: p.ID, DesignSystemID: p.DesignSystemID, SessionID: p.SessionID, State: p.State, BaseVersion: p.BaseVersion, Summary: p.Summary,
		Items: len(items), SubmittedAt: p.SubmittedAt, DecidedAt: p.DecidedAt,
	}
	for _, it := range items {
		if it.Conflicting {
			row.Conflicting++
		}
	}
	if s.sessionStore != nil {
		if sess := s.sessionStore.Get(p.SessionID); sess != nil {
			row.Agent, row.SessionRunning = s.agentName(sess.AgentID), !sess.Status.IsTerminal()
		}
	}
	return row
}

// GetDesignProposal implements ports.DesignProposals.
func (s *Service) GetDesignProposal(ctx context.Context, proposalID string) (*ports.DesignProposalView, error) {
	if s.proposals == nil {
		return nil, proposalNotFound()
	}
	p, err := s.proposals.Get(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, proposalNotFound()
	}
	_, cur, err := s.current(ctx, p.DesignSystemID)
	if err != nil {
		return nil, err
	}
	items, err := s.items(ctx, p, cur)
	if err != nil {
		return nil, err
	}
	if p.State != domain.ProposalDrafting && p.State != domain.ProposalOpen {
		for i := range items {
			items[i].Decision = decisionOf(p.State)
		}
	}
	view := &ports.DesignProposalView{DesignProposalRow: *s.row(p, items), Items: items, ProposedRecord: p.Draft}
	if cur != nil {
		view.CurrentVersion = cur.Version
		view.ProposedRecord = domain.ApplyDesignItems(cur.Record, items)
	} else {
		view.ProposedRecord = domain.ApplyDesignItems(domain.DesignSystemRecord{}, items)
	}
	view.AffectedComponents = affected(view.ProposedRecord, items)
	return view, nil
}

// decisionOf is the decision every item of a decided proposal has: a whole
// proposal is approved or rejected together.
func decisionOf(state string) string {
	switch state {
	case domain.ProposalApproved:
		return domain.DecisionApproved
	case domain.ProposalRejected, domain.ProposalWithdrawn:
		return domain.DecisionRejected
	}
	return domain.DecisionPending
}

// affected lists the components whose previews use a token the items change
// or remove, for before and after previews, with the items that touch them.
func affected(proposed domain.DesignSystemRecord, items []domain.DesignItem) []ports.AffectedComponent {
	var out []ports.AffectedComponent
	for _, c := range proposed.Components {
		var by []string
		for _, it := range items {
			if it.Kind != domain.ItemToken {
				continue
			}
			if previewsUse(c, tokenOf(it)) {
				by = append(by, it.ID)
			}
		}
		if len(by) > 0 {
			out = append(out, ports.AffectedComponent{Slug: c.Slug, ByItems: by})
		}
	}
	return out
}

// tokenOf is the token an item adds, changes or removes.
func tokenOf(it domain.DesignItem) domain.DesignToken {
	if t, ok := it.Proposed.(domain.DesignToken); ok {
		return t
	}
	t, _ := it.Base.(domain.DesignToken)
	return t
}

// previewsUse says a preview of the component names one of the token's custom
// properties.
func previewsUse(c domain.DesignComponent, t domain.DesignToken) bool {
	props := t.Properties()
	for _, p := range c.Previews {
		for _, prop := range props {
			if containsVar(p.CSS, prop) || containsVar(p.HTML, prop) {
				return true
			}
		}
	}
	return false
}

// containsVar finds var( --x ) with spaces too.
func containsVar(s, prop string) bool {
	for rest := s; ; {
		i := strings.Index(rest, "var(")
		if i < 0 {
			return false
		}
		rest = rest[i+len("var("):]
		if name, _, ok := strings.Cut(strings.TrimSpace(rest), ")"); ok && strings.TrimSpace(strings.SplitN(name, ",", 2)[0]) == prop {
			return true
		}
	}
}

// OpenProposals counts the project's design systems' open proposals, per
// design system.
func (s *Service) openProposals(ctx context.Context, designSystemID string) int {
	if s.proposals == nil {
		return 0
	}
	list, err := s.proposals.ListByDesignSystem(ctx, designSystemID)
	if err != nil {
		slog.Warn("count open design proposals", "design_system", designSystemID, "err", err)
		return 0
	}
	n := 0
	for _, p := range list {
		if p.State == domain.ProposalOpen {
			n++
		}
	}
	return n
}
