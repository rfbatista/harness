package orchestration

import (
	"context"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// designSessionTarget reads the design system a design session is started
// for and checks it belongs to project (skipped when project is nil, as on a
// resume, which has no request to check against).
func (s *Service) designSessionTarget(ctx context.Context, project *domain.Project, designSystemID string) (*domain.DesignSystem, error) {
	if designSystemID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "design_system_id is required"}
	}
	if s.catalog.DesignSystems == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "design systems are not available"}
	}
	ds, err := s.catalog.DesignSystems.GetDesignSystem(ctx, designSystemID)
	if err != nil {
		return nil, err
	}
	if project != nil && ds.ProjectID != project.ID {
		return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "design system not found"}
	}
	return ds, nil
}

// designSessionPrompt opens a design session: what it is, then the
// instruction the person typed (contract "Harness server ↔ Agent sessions —
// design system view, draft and proposals", "Session brief").
func designSessionPrompt(project *domain.Project, ds *domain.DesignSystem, typed string) string {
	var b strings.Builder
	b.WriteString("You are a design session on the design system \"" + ds.Name + "\" of project " + project.Name + ", started by the person. ")
	b.WriteString("Edit the draft in project/design-system/ of your virtual shell, then call submit_design_system_proposal with a summary; the person reviews the proposal and approves or rejects it.")
	if typed = strings.TrimSpace(typed); typed != "" {
		b.WriteString("\n\n" + typed)
	}
	return b.String()
}

// designSessionBriefInput is what a design session's brief is made from.
type designSessionBriefInput struct {
	Project *domain.Project
	// DesignSystem is the name of the design system the session is bound to.
	DesignSystem string
	Repositories []*domain.Repository
	Contexts     []*domain.BoundedContext
	// AgentsMove is the setting "Agents move documents to project" as the
	// session starts.
	AgentsMove bool
}

// designSessionTools are the task tools a design session may call: it reads
// the project around it, tells the person what they must know and hands its
// proposal over. It creates no task and starts no session: its only output is
// one proposal.
var designSessionTools = []string{
	"list_project_repositories", "list_bounded_contexts", "list_agents", "list_project_tasks",
	"list_notifications", "notify", "submit_design_system_proposal",
}

// applyDesignSessionContext is applyProjectContext for a design session: the
// per-session task MCP server and its allow-list (the tools that need a task
// answer SESSION_HAS_NO_TASK), and the design-session brief.
func applyDesignSessionContext(cfg *llmkit.SessionConfig, in designSessionBriefInput, taskServerURL string) {
	if in.Project == nil {
		return
	}
	if taskServerURL != "" {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{Name: taskServerName, Transport: "http", URL: taskServerURL})
	}
	for _, name := range designSessionTools {
		cfg.AllowedTools = append(cfg.AllowedTools, llmkit.MCPToolRef(taskServerName, name))
	}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, designSessionBrief(in))
}

// designSessionBrief is the block describing what a design session is, what
// it may change and how it hands its work over.
func designSessionBrief(in designSessionBriefInput) string {
	var b strings.Builder
	b.WriteString("## Your design session\n\n")
	b.WriteString("You are a design session on the design system \"" + in.DesignSystem + "\" of project " + in.Project.Name + ", started by the person.\n")
	b.WriteString("You change no repository and have no task. Your only output is one proposal: you edit the design system's draft and call mcp__task__submit_design_system_proposal; the person approves or rejects it as a whole.\n")
	if len(in.Repositories) > 0 {
		b.WriteString("\nRepositories (read them to see how the look is used; you cannot change them):\n\n")
		for _, r := range in.Repositories {
			line := "- " + r.Name
			if d := strings.TrimSpace(r.Description); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	if len(in.Contexts) > 0 {
		b.WriteString("\nBounded contexts:\n\n")
		for _, c := range in.Contexts {
			line := "- " + c.Name
			if d := strings.TrimSpace(c.Purpose); d != "" {
				line += " — " + d
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(`
The draft and the project's documents and designs are files in your virtual
shell, $HARNESS_SHELL ("Your virtual shell" below names its path; its README.md
maps the layout and the rules). Work on them with your ordinary tools; none of
it takes a tool call:

- Read with Read, grep, ls and sed -n. .index.json lists every file with its id, kind, scope and version.
- Write with Write, Edit, sed -i, tee, cp, mv, rm, touch and mkdir. Each change is checked before it lands and stored once it has. Shell redirects (>, >>) into the folder are refused: they would not be stored.

What the folders hold:

- project/design-system/ — the draft of this design system, the only design system you may change. Tokens are edited in tokens.json, principles in the body of DESIGN.md (its frontmatter is generated), each component in components/<slug>.html with its previews. README.md is generated. A write that would make the draft invalid is refused with the list of errors; after each accepted write the files show what would be exported.
- project/docs/ — the project's documents: architecture, conventions, decisions. Read them. ` + projectMoveLine(in.AgentsMove) + `
- project/designs/ — the project's design assets. Read-only here.

This session has no task, so there is no task/ folder.

A refused write says why, starting with its code:

- DESIGN_SYSTEM_INVALID — the draft would be invalid: the message lists each error
- PROPOSAL_SUBMITTED — your proposal waits on the person; it returns to you if they send items back
- READ_ONLY — the path is not yours to change
- BASE_VERSION_STALE — the file changed since you got it and has been refreshed: read it again and redo the change
- REDIRECT_DENIED — write with Write, Edit or tee instead of > or >>
- NOT_STORED — the path is outside the folders the harness stores; nothing was written
- SHELL_UNAVAILABLE — the harness could not be reached: redo the change once it is back

When the draft says what you want, call:

- mcp__task__submit_design_system_proposal — summary: submit the draft as your proposal. Refused when the draft equals its base or a proposal of yours already waits. The person's decision comes back to you as a turn, and your draft then restarts from the current version

To see the project around the design:

- mcp__task__list_project_repositories — the project's repositories: its applications
- mcp__task__list_bounded_contexts — its bounded contexts, with their language and zones

The tools that act on a session's own task answer SESSION_HAS_NO_TASK here.
These tools always act on this session's project; they take no project id.`)
	return b.String() + "\n\n" + notificationsSection
}

// StartDesignSession starts a design session bound to the design system, as
// agentID, on its project with no task, running on the server. The session
// reads every repository and changes none; its worktree is a read-only one of
// the project's first repository, which every session has (PROJECT_NOT_FOUND
// style refusals aside, NO_REPOSITORY when the project has none).
func (s *Service) StartDesignSession(ctx context.Context, designSystemID, agentID, prompt string) (*domain.Session, error) {
	ds, err := s.designSessionTarget(ctx, nil, designSystemID)
	if err != nil {
		return nil, err
	}
	repos, err := s.catalog.Repositories.ListRepositories(ctx, ds.ProjectID)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, &domain.StructuredError{Code: "NO_REPOSITORY", Message: "the project has no repository: add one before starting a design session"}
	}
	sess, _, err := s.StartInteractive(ctx, ports.InteractiveRequest{
		ProjectID: ds.ProjectID, RepositoryID: repos[0].ID, AgentID: agentID, Prompt: prompt,
		Mode: string(domain.SessionModeDesignSystem), DesignSystemID: designSystemID, RunsOn: domain.RunnerServer,
	})
	return sess, err
}

// RunningDesignSessions lists the ids of the design sessions bound to the
// design system that have not ended.
func (s *Service) RunningDesignSessions(ctx context.Context, designSystemID string) []string {
	var ids []string
	for _, sess := range s.sessions.List(ctx, ports.SessionFilter{DesignSystemID: designSystemID}) {
		if !sess.Status.IsTerminal() {
			ids = append(ids, sess.ID)
		}
	}
	return ids
}
