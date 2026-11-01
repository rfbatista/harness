package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Option adds a tool group to SessionTaskTools that not every server has.
type Option func(*options)

type options struct {
	proposals ports.DesignProposalSubmitter
}

// WithDesignProposals lets a design session submit its draft as its proposal.
// Without it submit_design_system_proposal answers that proposals are
// unavailable.
func WithDesignProposals(p ports.DesignProposalSubmitter) Option {
	return func(o *options) { o.proposals = p }
}

// sessionDesignProposalTools is submit_design_system_proposal: the one way a
// design session hands its work over (contract "Harness server ↔ Agent
// sessions — design-system view, draft and proposals", "Session tool").
func sessionDesignProposalTools(sessions ports.SessionRepository, sub ports.DesignProposalSubmitter) []domain.Tool {
	return []domain.Tool{{
		Name: "submit_design_system_proposal",
		Description: "Design sessions only: submit your draft of the design system, project/design-system/ in your virtual shell, as your one proposal. " +
			"The person reviews each change and approves or rejects the proposal as a whole; until they decide you cannot write the draft. " +
			"Their decision comes back to you as a turn, and your draft then restarts from the current version. " +
			"Refused with NOT_A_DESIGN_SESSION in any other session, PROPOSAL_EMPTY when your draft equals its base, " +
			"PROPOSAL_SUBMITTED when a proposal of yours already waits. Answers the proposal id, how many items it holds and how many conflict " +
			"with changes the person made since your draft's base.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["summary"],"properties":{` +
			`"summary":{"type":"string","description":"What the proposal changes and why, in a line or two."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			sess, err := resolveProjectScope(ctx, sessions)
			if err != nil {
				return nil, err
			}
			if sub == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "design proposals are not available on this server"}
			}
			got, err := sub.SubmitDesignProposal(ctx, sess.ID, getString(args, "summary", ""))
			if err != nil {
				return nil, err
			}
			return map[string]any{"proposal_id": got.ProposalID, "items": got.Items, "conflicting": got.Conflicting}, nil
		},
	}}
}
