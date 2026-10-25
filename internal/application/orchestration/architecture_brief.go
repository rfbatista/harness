package orchestration

import (
	"context"
	"log/slog"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// architectureSection is the brief's part about the repository architecture
// (contract "Harness server ↔ Agent sessions — repository architecture
// record"): for a session working in a repository that has a record, where to
// read it and which layers its agent may change; for an architect, every
// record of the project. A repository with no record adds nothing. The brief
// is fixed when the session starts: rules apply live, the text does not.
func (s *Service) architectureSection(ctx context.Context, role domain.SessionRole, agent *domain.Agent, projectID, repositoryID string) string {
	if s.Architectures == nil || projectID == "" {
		return ""
	}
	refs, err := s.Architectures.ListArchitectureRefs(ctx, projectID)
	if err != nil {
		slog.Warn("list the project's architectures for a brief", "project", projectID, "err", err)
		return ""
	}
	if role == domain.RoleArchitect {
		return architectBriefSection(refs)
	}
	for _, ref := range refs {
		if ref.RepositoryID != repositoryID {
			continue
		}
		rec, err := s.Architectures.SavedArchitecture(ctx, repositoryID)
		if err != nil || rec == nil {
			return ""
		}
		name := domain.PlainAgent
		if agent != nil && agent.Name != "" {
			name = agent.Name
		}
		return agentBriefSection(ref, rec, name)
	}
	return ""
}

// architectBriefSection lists every record: an architect reads them while it
// shapes the design and proposes changes in its spec, never in the record.
func architectBriefSection(refs []ports.ArchitectureRef) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Repository architecture\n\n")
	b.WriteString("These repositories of the project have an architecture record, a read-only file in your virtual shell:\n\n")
	for _, r := range refs {
		b.WriteString("- " + r.Path + " — " + r.Repository + "\n")
	}
	b.WriteString("\nRead the relevant ones while you grill the design. When the design changes a layer, its access or a rule, write the proposed change as a section of the spec, for the person to apply in the web UI. You never edit a record.")
	return b.String()
}

// agentBriefSection is the section of a session working in the repository of
// ref: which layers its agent may change, how the rules behave, and which
// layers it may not read.
func agentBriefSection(ref ports.ArchitectureRef, rec *domain.ArchitectureRecord, agent string) string {
	var change, unreadable []string
	for _, l := range rec.Layers {
		switch l.Access.For(agent) {
		case domain.AccessChange:
			change = append(change, l.Name)
		case domain.AccessNone:
			unreadable = append(unreadable, l.Name)
		}
	}
	var b strings.Builder
	b.WriteString("## Repository architecture\n\n")
	b.WriteString("The repository " + ref.Repository + " has an architecture record: " + ref.Path + " in your virtual shell. Read it before you change code.\n\n")
	if agent == domain.PlainAgent {
		b.WriteString("You run with no agent. ")
	} else {
		b.WriteString("You work as " + agent + ". ")
	}
	switch {
	case len(rec.Layers) > 0 && len(change) == len(rec.Layers):
		b.WriteString("You may change every layer.")
	case len(change) == 0:
		b.WriteString("You may change no layer.")
	default:
		b.WriteString("You may change these layers: " + strings.Join(change, ", ") + ".")
	}
	if rec.Unlayered.For(agent) == domain.AccessChange {
		b.WriteString(" You may change paths that are in no layer.")
	} else {
		b.WriteString(" You may not change paths that are in no layer.")
	}
	b.WriteString("\n\nArchitecture rules guard your edits and your commits. A blocking rule denies the action and tells you why; a warning reaches you as context. Ask your architect (or the person, when you have no architect) before you work around a rule.")
	if len(unreadable) > 0 {
		b.WriteString("\n\nYou may not read these layers: " + strings.Join(unreadable, ", ") + ". This is not enforced, but respect it.")
	}
	return b.String()
}
