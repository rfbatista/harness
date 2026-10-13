package tooling

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionArtifactScopeTools attach the project's design assets to a task
// session's task, and detach them. Deletion of a project asset stays with
// people.
func sessionArtifactScopeTools(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) []domain.Tool {
	// link attaches or detaches a project asset to or from this session's task.
	link := func(name, description string, apply func(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error)) domain.Tool {
		return domain.Tool{
			Name:        name,
			Description: description,
			InputSchema: schemaFromJSON(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string","description":"A project asset's artifact_id: its id in .index.json in your virtual shell (project/designs/)."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Attachments == nil {
					return nil, artifactsUnavailable()
				}
				a, err := apply(ctx, getString(args, "artifact_id", ""), scope.ticket.ID)
				if err != nil {
					return nil, codedMessage(err)
				}
				title := ""
				if tk, err := planningSvc.GetTicket(ctx, a.TicketID); err == nil && tk != nil {
					title = tk.Title
				}
				return map[string]any{"artifact": reportProjectArtifact(art, a, title)}, nil
			},
		}
	}
	return []domain.Tool{
		link("attach_artifact_to_task",
			"Attach a project design asset, from any task of this project, to this task: it then shows in this task's Design tab "+
				"and in task/designs/attached.md in your virtual shell. Only a project asset can be attached "+
				"(ARTIFACT_NOT_IN_PROJECT: its task moves it to the project first), and only within its project "+
				"(ARTIFACT_PROJECT_MISMATCH). Attaching again, or to the task that produced it, changes nothing.",
			func(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
				return art.Attachments.AttachArtifactToTicket(ctx, artifactID, ticketID)
			}),
		link("detach_artifact_from_task",
			"Detach a project design asset from this task; it stays in the project. The task that produced it cannot detach it "+
				"(ARTIFACT_PRODUCER_TASK). Detaching one that is not attached changes nothing.",
			func(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
				return art.Attachments.DetachArtifactFromTicket(ctx, artifactID, ticketID)
			}),
	}
}

// projectArtifact is an artifact as the scope tools report it.
type projectArtifact struct {
	ArtifactID string               `json:"artifact_id"`
	Title      string               `json:"title"`
	Kind       domain.ArtifactKind  `json:"kind"`
	Note       string               `json:"note,omitempty"`
	Revision   int                  `json:"revision"`
	Bytes      int64                `json:"bytes"`
	Scope      domain.ArtifactScope `json:"scope"`
	TaskID     string               `json:"task_id"`
	TaskTitle  string               `json:"task_title,omitempty"`
	ViewURL    string               `json:"view_url"`
	UpdatedAt  time.Time            `json:"updated_at"`
	// AttachedTicketIDs are the other tasks the asset is attached to.
	AttachedTicketIDs []string `json:"attached_ticket_ids"`
}

func reportProjectArtifact(art ArtifactTooling, a *domain.Artifact, taskTitle string) projectArtifact {
	return projectArtifact{ArtifactID: a.ID, Title: a.Title, Kind: a.Kind, Note: a.Note, Revision: a.Revision, Bytes: a.SizeBytes,
		Scope: scopeOf(a), TaskID: a.TicketID, TaskTitle: taskTitle, ViewURL: viewURL(art, a), UpdatedAt: a.UpdatedAt,
		AttachedTicketIDs: attachedIDs(a)}
}

// scopeOf reads an artifact's scope; one that predates scopes is a task's.
func scopeOf(a *domain.Artifact) domain.ArtifactScope {
	if a.Scope == "" {
		return domain.ArtifactScopeTask
	}
	return a.Scope
}
