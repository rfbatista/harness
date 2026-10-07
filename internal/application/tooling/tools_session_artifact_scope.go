package tooling

import (
	"context"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionArtifactScopeTools reach the project's design assets from a task
// session: the artifacts moved to project scope, which every session of the
// project may list, and the moves themselves, which a session may only make
// on artifacts published on its own task. Deletion of a project asset stays
// with people.
func sessionArtifactScopeTools(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) []domain.Tool {
	move := func(name, description string, to domain.ArtifactScope) domain.Tool {
		return domain.Tool{
			Name:        name,
			Description: description,
			InputSchema: schemaFromJSON(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string","description":"An artifact_id from publish_artifact or list_task_artifacts."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, artifactsUnavailable()
				}
				// Only an artifact published on this session's task moves; its task stays.
				current, err := scope.artifact(ctx, art.Publisher, getString(args, "artifact_id", ""))
				if err != nil {
					return nil, err
				}
				a, err := art.Publisher.SetArtifactScope(ctx, current.ID, to)
				if err != nil {
					return nil, codedMessage(err)
				}
				return map[string]any{"artifact": reportProjectArtifact(art, a, scope.ticket.Title)}, nil
			},
		}
	}
	return []domain.Tool{
		{
			Name: "list_project_artifacts",
			Description: "List the design assets kept at project level in this session's project, from any task, newest first: " +
				"logos, palettes, components, reference screens other tasks reuse. Each has its artifact_id, title, kind, note, " +
				"revision, the task that made it, and a view_url. Check it before making a new asset.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, artifactsUnavailable()
				}
				list, err := art.Publisher.ListProjectArtifacts(ctx, scope.session.ProjectID)
				if err != nil {
					return nil, err
				}
				titles := map[string]string{}
				out := make([]projectArtifact, 0, len(list))
				for _, a := range list {
					if a.Scope != domain.ArtifactScopeProject {
						continue
					}
					title, ok := titles[a.TicketID]
					if !ok {
						// A task deleted since leaves its assets with no title.
						if tk, err := planningSvc.GetTicket(ctx, a.TicketID); err == nil && tk != nil {
							title = tk.Title
						}
						titles[a.TicketID] = title
					}
					out = append(out, reportProjectArtifact(art, a, title))
				}
				return map[string]any{"count": len(out), "artifacts": out}, nil
			},
		},
		move("move_artifact_to_project",
			"Make one of this task's artifacts a project design asset: the harness keeps its own copy, so it outlives this session "+
				"and its worktree, and it shows in the project's design-assets library for every task. Do it for assets other tasks "+
				"will reuse (a logo, a palette, a component, a reference screen). The artifact must be published on this task "+
				"(ARTIFACT_NOT_ON_TASK otherwise); a dev-server url cannot move (ARTIFACT_NOT_PROMOTABLE). Re-publishing the same "+
				"path from the publishing session refreshes it. Idempotent.",
			domain.ArtifactScopeProject),
		move("move_artifact_to_task",
			"Move a project design asset published on this task back to task scope, for example before unpublishing it. "+
				"The artifact must be published on this task (ARTIFACT_NOT_ON_TASK otherwise). Idempotent.",
			domain.ArtifactScopeTask),
	}
}

// artifact returns the artifact only when it was published on the scoped task
// (in either scope), so a session cannot move another task's assets.
func (s *taskScope) artifact(ctx context.Context, pub ports.ArtifactPublisher, artifactID string) (*domain.Artifact, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "artifact_id is required"}
	}
	list, err := pub.ListTaskArtifacts(ctx, s.ticket.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.ID == artifactID {
			return a, nil
		}
	}
	return nil, codedMessage(&domain.StructuredError{Code: "ARTIFACT_NOT_ON_TASK", Message: "artifact was not published on this session's task"})
}

// projectArtifact is an artifact as the scope tools report it.
type projectArtifact struct {
	ArtifactID string               `json:"artifact_id"`
	Title      string               `json:"title"`
	Kind       domain.ArtifactKind  `json:"kind"`
	Note       string               `json:"note,omitempty"`
	Revision   int                  `json:"revision"`
	Scope      domain.ArtifactScope `json:"scope"`
	TaskID     string               `json:"task_id"`
	TaskTitle  string               `json:"task_title,omitempty"`
	ViewURL    string               `json:"view_url"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

func reportProjectArtifact(art ArtifactTooling, a *domain.Artifact, taskTitle string) projectArtifact {
	return projectArtifact{ArtifactID: a.ID, Title: a.Title, Kind: a.Kind, Note: a.Note, Revision: a.Revision,
		Scope: scopeOf(a), TaskID: a.TicketID, TaskTitle: taskTitle, ViewURL: viewURL(art, a), UpdatedAt: a.UpdatedAt}
}

// scopeOf reads an artifact's scope; one that predates scopes is a task's.
func scopeOf(a *domain.Artifact) domain.ArtifactScope {
	if a.Scope == "" {
		return domain.ArtifactScopeTask
	}
	return a.Scope
}
