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
func sessionProjectTools(planningSvc ports.Planning, sessions ports.SessionRepository, agents ports.AgentLister, arch ports.ArchitectureMap, repos ports.RepositoryLister) []domain.Tool {
	noArgs := schemaFromJSON(`{"type":"object","properties":{}}`)
	return []domain.Tool{
		{
			Name: "list_project_repositories",
			Description: "List the repositories of this task's project — each is an application the task may touch. " +
				"Use a repository's name as start_task_session's repository.",
			InputSchema: noArgs,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if repos == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "repositories are not available on this server"}
				}
				list, err := repos.ListRepositories(ctx, scope.session.ProjectID)
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
						"yours":       r.ID == scope.session.RepositoryID,
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
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if arch == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the project's architecture is not available on this server"}
				}
				return architectureMap(arch, scope.session.ProjectID), nil
			},
		},
		{
			Name: "list_agents",
			Description: "List the agents a session can run as — name, what each is for, and its skills. " +
				"Pick one by its description and pass its name as start_task_session's agent.",
			InputSchema: noArgs,
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if _, err := resolveTaskScope(ctx, planningSvc, sessions); err != nil {
					return nil, err
				}
				if agents == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "no agents are available on this server"}
				}
				list, err := agents.ListAgents(ctx)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, a := range list {
					skills := make([]string, 0, len(a.Skills))
					for _, sk := range a.Skills {
						skills = append(skills, sk.Name)
					}
					out = append(out, map[string]any{
						"id":          a.ID,
						"name":        a.Name,
						"description": a.Description,
						"skills":      skills,
					})
				}
				return map[string]any{"count": len(out), "agents": out}, nil
			},
		},
	}
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
func architectureMap(arch ports.ArchitectureMap, projectID string) map[string]any {
	byContext := map[string][]zoneSummary{}
	unassigned := []zoneSummary{}
	for _, z := range arch.ListZones(projectID) {
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
