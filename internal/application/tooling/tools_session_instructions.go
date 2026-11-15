package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WithInstructionProposals serves propose_instruction_files, with which a
// session submits the skills it drafted for a repository (ADR 0092). Without it
// the tool answers that it is unavailable.
func WithInstructionProposals(p ports.InstructionProposalSubmitter) Option {
	return func(o *options) { o.instructions = p }
}

// sessionInstructionTools is propose_instruction_files (contract "Harness
// server ↔ Agent sessions — instruction-file proposals", §2).
func sessionInstructionTools(sessions ports.SessionRepository, sub ports.InstructionProposalSubmitter) []domain.Tool {
	return []domain.Tool{{
		Name: "propose_instruction_files",
		Description: "Submit skills you drafted in task/instruction-drafts/<repository>/<skill>/ of your virtual shell, as one proposal for the person " +
			"to approve as the repository's instruction files. A draft of an existing skill replaces its tree: files you left out of the draft are removed. " +
			"Skills only, never memory files or sub-agents. Until the person decides, the submitted skills' draft folders are read-only to you; " +
			"their decision, or their note when they send it back, reaches you as a turn. Answers the proposal id and how many files are created, changed and removed. " +
			"Refused with INVALID_INPUT, REPOSITORY_NOT_FOUND, INSTRUCTION_INVALID, PROPOSAL_EMPTY or PROPOSAL_SUBMITTED, the code first in the message.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["repository","skills","summary"],"properties":{` +
			`"repository":{"type":"string","description":"The repository's name, as list_project_repositories gives it."},` +
			`"skills":{"type":"array","items":{"type":"string"},"description":"The draft skill folders to submit together."},` +
			`"summary":{"type":"string","description":"What the skills are for, in a line or two."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			sess, err := resolveProjectScope(ctx, sessions)
			if err != nil {
				return nil, err
			}
			if sub == nil {
				return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "instruction-file proposals are not available on this server"}
			}
			got, err := sub.SubmitInstructionProposal(ctx, sess.ID, getString(args, "repository", ""), getStringSlice(args, "skills"), getString(args, "summary", ""))
			if err != nil {
				return nil, err
			}
			return map[string]any{"proposal_id": got.ProposalID, "files": got.Files, "created": got.Created, "changed": got.Changed, "removed": got.Removed}, nil
		},
	}}
}
