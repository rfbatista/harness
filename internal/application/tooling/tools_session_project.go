package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionProjectTools let a task session see the project around its task: the
// repositories (its applications), how the code is carved into bounded
// contexts and zones, and the agents it can hand work to. They are read-only
// and, like the task tools, scoped by the session id, never by arguments.
func sessionProjectTools(sessions ports.SessionRepository, agents ports.AgentLister, arch ports.ArchitectureMap, repos ports.RepositoryLister, placements ports.AgentPlacements, modeAgents ports.SettingsReader) []domain.Tool {
	noArgs := schemaFromJSON(`{"type":"object","properties":{}}`)
	return []domain.Tool{
		{
			Name: "list_project_repositories",
			Description: "List the repositories of this task's project — each is an application the task may touch. " +
				"Use a repository's name as start_task_session's repository.",
			InputSchema: noArgs,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, sessions)
				if err != nil {
					return nil, err
				}
				if repos == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "repositories are not available on this server"}
				}
				list, err := repos.ListRepositories(ctx, sess.ProjectID)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, r := range list {
					out = append(out, map[string]any{
						"id":          r.ID,
						"name":        r.Name,
						"description": r.Description,
						"url":         r.URL,
						"yours":       r.ID == sess.RepositoryID,
					})
				}
				return map[string]any{"count": len(out), "repositories": out}, nil
			},
		},
		{
			Name: "list_bounded_contexts",
			Description: "List the bounded contexts of this task's project — each with its purpose, ubiquitous language " +
				"and the zones (path patterns) that belong to it — plus the zones in no context.",
			InputSchema: noArgs,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, sessions)
				if err != nil {
					return nil, err
				}
				if arch == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the project's architecture is not available on this server"}
				}
				return architectureMap(ctx, arch, sess.ProjectID), nil
			},
		},
		{
			Name: "list_agents",
			Description: "List the agents a session can run as — name, what each is for, and its skills. " +
				"Each also names the repositories it is unassigned from (unassigned_from): start_task_session is refused there with AGENT_NOT_ASSIGNED, and only the person can assign it again. " +
				"Each also names the session modes it cannot start in (unavailable_in_modes, mode values; \"\" is the default mode), and no_agent_unavailable_in_modes the modes that refuse a session with no agent: " +
				"start_task_session is refused in those modes with AGENT_NOT_AVAILABLE_IN_MODE, and only the person changes them. " +
				"Pick one by its description and pass its name as start_task_session's agent.",
			InputSchema: noArgs,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, err := resolveProjectScope(ctx, sessions)
				if err != nil {
					return nil, err
				}
				if agents == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "no agents are available on this server"}
				}
				list, err := agents.ListAgents(ctx)
				if err != nil {
					return nil, err
				}
				var unassigned map[string][]domain.RepositoryRef
				if placements != nil {
					if unassigned, err = placements.UnassignedFrom(ctx, sess.ProjectID); err != nil {
						return nil, err
					}
				}
				var modes []domain.ModeAgents
				if modeAgents != nil {
					modes = domain.ModeAgentsList(modeAgents.Setting)
				}
				out := make([]map[string]any, 0, len(list))
				for _, a := range list {
					skills := make([]string, 0, len(a.Skills))
					for _, sk := range a.Skills {
						skills = append(skills, sk.Name)
					}
					entry := map[string]any{
						"id":          a.ID,
						"name":        a.Name,
						"description": a.Description,
						"skills":      skills,
					}
					if placements != nil {
						entry["unassigned_from"] = append([]domain.RepositoryRef{}, unassigned[a.Name]...)
					}
					if modeAgents != nil {
						entry["unavailable_in_modes"] = unavailableModes(modes, a.Name)
					}
					out = append(out, entry)
				}
				answer := map[string]any{"count": len(out), "agents": out}
				if modeAgents != nil {
					answer["no_agent_unavailable_in_modes"] = unavailableModes(modes, "")
				}
				return answer, nil
			},
		},
	}
}

// unavailableModes are the mode values of the modes that refuse the agent
// (a session with no agent when agent is ""), in the order of the modes.
func unavailableModes(modes []domain.ModeAgents, agent string) []string {
	out := []string{}
	for _, m := range modes {
		if !m.Allows(agent) {
			out = append(out, string(m.Mode))
		}
	}
	return out
}

type zoneSummary struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

type contextSummary struct {
	Name               string                `json:"name"`
	Purpose            string                `json:"purpose,omitempty"`
	UbiquitousLanguage []domain.LanguageTerm `json:"ubiquitous_language,omitempty"`
	Zones              []zoneSummary         `json:"zones"`
}

// architectureMap groups a project's zones under their bounded contexts.
func architectureMap(ctx context.Context, arch ports.ArchitectureMap, projectID string) map[string]any {
	byContext := map[string][]zoneSummary{}
	unassigned := []zoneSummary{}
	for _, z := range arch.ListZones(ctx, projectID) {
		zs := zoneSummary{Name: z.Name, Pattern: z.Pattern, Purpose: z.Purpose}
		if z.BoundedContextID == "" {
			unassigned = append(unassigned, zs)
			continue
		}
		byContext[z.BoundedContextID] = append(byContext[z.BoundedContextID], zs)
	}
	contexts := []contextSummary{}
	for _, bc := range arch.ListBoundedContexts(projectID) {
		zones := byContext[bc.ID]
		if zones == nil {
			zones = []zoneSummary{}
		}
		contexts = append(contexts, contextSummary{Name: bc.Name, Purpose: bc.Purpose, UbiquitousLanguage: bc.UbiquitousLanguage, Zones: zones})
	}
	return map[string]any{"bounded_contexts": contexts, "unassigned_zones": unassigned}
}
