package archassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// proposalBlocks finds the fenced json blocks of an answer.
var proposalBlocks = regexp.MustCompile("(?s)```json[ \t]*\r?\n(.*?)```")

// proposalJSON is the assistant's block: the server fills in each change's
// base and the proposal's id.
type proposalJSON struct {
	Summary string                  `json:"summary"`
	Changes []domain.ProposalChange `json:"changes"`
}

// parseProposal reads the last fenced json block of text as a proposal. An
// answer with no such block, or whose last block is not a proposal with at
// least one change, is a plain answer: nil.
func parseProposal(text string) *proposalJSON {
	blocks := proposalBlocks.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		return nil
	}
	var p proposalJSON
	if err := json.Unmarshal([]byte(blocks[len(blocks)-1][1]), &p); err != nil || len(p.Changes) == 0 {
		return nil
	}
	return &p
}

// conclude turns a finished answer into the turn's result: a plain answer, or
// a proposal checked against the person's draft. A proposal that breaks the
// record's validation goes back to the assistant once, in the same
// conversation, and the corrected one is checked in turn; if that fails too the
// proposal is returned with valid false and its errors.
func (s *Service) conclude(ctx context.Context, repo *domain.Repository, conv *conversation, draft domain.ArchitectureRecord, answer TurnResult, emit func(ports.ArchitectureAssistantEvent)) (ports.ArchitectureAssistantResult, error) {
	first := parseProposal(answer.Text)
	if first == nil {
		return ports.ArchitectureAssistantResult{Text: answer.Text}, nil
	}
	known, err := s.known(ctx, repo.ProjectID)
	if err != nil {
		return ports.ArchitectureAssistantResult{}, err
	}
	checked, errs := check(draft, known, first)
	if len(errs) == 0 {
		return s.proposed(conv, answer.Text, first.Summary, checked, nil), nil
	}

	retry, err := s.turn(ctx, repo, conv, retryInput(errs), emit, false)
	if err != nil {
		if ctx.Err() != nil {
			return ports.ArchitectureAssistantResult{}, ctx.Err()
		}
		return s.proposed(conv, answer.Text, first.Summary, checked, errs), nil
	}
	second := parseProposal(retry.Text)
	if second == nil {
		return s.proposed(conv, answer.Text, first.Summary, checked, errs), nil
	}
	checked, errs = check(draft, known, second)
	return s.proposed(conv, retry.Text, second.Summary, checked, errs), nil
}

func (s *Service) proposed(conv *conversation, text, summary string, changes []domain.ProposalChange, errs []domain.ArchFieldError) ports.ArchitectureAssistantResult {
	s.mu.Lock()
	conv.proposals++
	id := fmt.Sprintf("p%d", conv.proposals)
	s.mu.Unlock()
	if errs == nil {
		errs = []domain.ArchFieldError{}
	}
	return ports.ArchitectureAssistantResult{Text: text, Proposal: &ports.ArchitectureAssistantProposal{
		ID: id, Summary: summary, Valid: len(errs) == 0, Changes: changes, Errors: errs,
	}}
}

// check fills in each change's base from draft, gives a new rule without a mode
// the warning mode, applies the changes to draft and validates the result with
// the record's own validation. It answers the changes as completed and the
// problems, none when the proposal is valid.
func check(draft domain.ArchitectureRecord, known domain.ArchitectureKnown, p *proposalJSON) ([]domain.ProposalChange, []domain.ArchFieldError) {
	changes := make([]domain.ProposalChange, len(p.Changes))
	copy(changes, p.Changes)
	var errs []domain.ArchFieldError
	for i := range changes {
		c := &changes[i]
		if base, err := domain.ProposalBase(draft, c.Target, c.Op); err == nil {
			c.Base = base
		} else {
			c.Base = json.RawMessage("null")
			errs = append(errs, changeError(i, err))
		}
		if c.Target.Kind == domain.TargetRule && c.Op == domain.OpAdd {
			c.Value = withDefaultMode(c.Value)
		}
	}
	if len(errs) > 0 {
		return changes, errs
	}
	applied, err := domain.ApplyProposal(draft, changes)
	if err != nil {
		var ae *domain.ProposalApplyError
		if errors.As(err, &ae) {
			return changes, []domain.ArchFieldError{changeError(ae.Index, ae.Err)}
		}
		return changes, []domain.ArchFieldError{changeError(0, err)}
	}
	applied.Normalize()
	return changes, applied.Validate(known)
}

func changeError(i int, err error) domain.ArchFieldError {
	return domain.ArchFieldError{Path: fmt.Sprintf("/changes/%d", i), Code: domain.ArchInvalidValue, Message: err.Error()}
}

// withDefaultMode sets mode warning on a rule value that has none, so a rule
// the person did not ask to block cannot block agents.
func withDefaultMode(value json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(value, &m) != nil {
		return value
	}
	if mode, ok := m["mode"]; ok && string(mode) != `""` && string(mode) != "null" {
		return value
	}
	m["mode"] = json.RawMessage(`"warning"`)
	out, err := json.Marshal(m)
	if err != nil {
		return value
	}
	return out
}

// retryInput is the follow-up turn that sends the validation errors back.
func retryInput(errs []domain.ArchFieldError) string {
	var b strings.Builder
	b.WriteString("<validation_errors>\nYour proposal broke the record's validation once applied to the draft. Each path is a JSON pointer into the draft with your changes applied, or /changes/N for the N-th change (from 0) that could not be applied:\n")
	for _, e := range errs {
		fmt.Fprintf(&b, "- %s %s: %s\n", e.Path, e.Code, e.Message)
	}
	b.WriteString("</validation_errors>\n\n<message>\nAnswer again with the corrected proposal, in the same format: one fenced json block at the end, with every change that was right kept as it was.\n</message>\n")
	return b.String()
}

// known is what a record's names are checked against: the agent catalog and the
// project's bounded contexts, as a save checks them.
func (s *Service) known(ctx context.Context, projectID string) (domain.ArchitectureKnown, error) {
	k := domain.ArchitectureKnown{Agents: map[string]bool{}, BoundedContexts: map[string]bool{}}
	agents, err := s.Agents.ListAgents(ctx)
	if err != nil {
		return k, err
	}
	for _, a := range agents {
		k.Agents[a.Name] = true
	}
	if s.Contexts != nil {
		for _, bc := range s.Contexts.ListBoundedContexts(projectID) {
			k.BoundedContexts[bc.Name] = true
		}
	}
	return k, nil
}
