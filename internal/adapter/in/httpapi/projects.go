package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// handleListProjectSummaries answers every project at a glance, in one call.
func (h *Handler) handleListProjectSummaries(c echo.Context) error {
	list, err := h.projects.ListProjectSummaries(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ListProjectSummariesOut{Summaries: mcp.ProjectSummariesToDTO(list)})
}

// projectEvent is one change on /api/project_events: a project as
// list_projects shows it, or only its id with deleted set.
type projectEvent struct {
	Project any  `json:"project"`
	Deleted bool `json:"deleted,omitempty"`
}

func projectEventOf(c ports.ProjectCatalogChange) projectEvent {
	if c.Deleted {
		return projectEvent{Project: map[string]string{"id": c.Project.ID}, Deleted: true}
	}
	return projectEvent{Project: mcp.ProjectToDTO(c.Project)}
}

// handleProjectEvents streams the set of projects as it changes — created,
// updated (ignored paths included), deleted — and the git credentials and
// identity as they change, as server-sent events, one change per event. Pings and the end of a stream that fell behind work as on
// /api/events.
func (h *Handler) handleProjectEvents(c echo.Context) error {
	changes, err := h.followCatalog(c.Request().Context())
	if err != nil {
		return err
	}
	return h.streamSSE(c, changes)
}

func (h *Handler) followCatalog(ctx context.Context) (<-chan any, error) {
	if h.projectFeed == nil {
		return nil, echo.NewHTTPError(http.StatusServiceUnavailable, "project feed not configured")
	}
	changes, err := h.projectFeed.FollowProjects(ctx)
	if err != nil {
		return nil, err
	}
	var creds <-chan ports.GitCredentialChange
	if h.gitCredentials != nil {
		creds, err = h.gitCredentials.FollowGitCredentials(ctx)
		if err != nil {
			return nil, err
		}
	}
	var catalogChanges <-chan domain.CatalogChanged
	if feed, ok := h.catalog.(ports.CatalogChangeFeed); ok {
		catalogChanges, err = feed.FollowCatalogChanges(ctx)
		if err != nil {
			return nil, err
		}
	}
	if creds == nil && catalogChanges == nil {
		return relayAs(ctx, changes, func(ch ports.ProjectCatalogChange) any { return projectEventOf(ch) }), nil
	}
	return catalogEvents(ctx, changes, creds, catalogChanges), nil
}
