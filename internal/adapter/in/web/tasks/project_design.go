package tasks

import (
	"context"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ProjectDesignHref is the project's design-assets library.
func ProjectDesignHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/design"
}

// ProjectDesignView is the frame of the project's design assets. The browser
// lists them from the artifacts API (sessionsDesignLibrary); the server seeds
// the project and its tasks, so each asset can name the task it came from.
type ProjectDesignView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	Seed        DesignLibrarySeed
}

// DesignLibrarySeed is the design-library-seed JSON.
type DesignLibrarySeed struct {
	ProjectID string             `json:"project_id"`
	Tasks     []DesignLibraryTask `json:"tasks"`
	// Artifacts are the project's assets, each with the tasks it is attached
	// to. Never null.
	Artifacts []*domain.Artifact `json:"artifacts"`
}

// DesignLibraryTask is a task an asset may come from or be attached to,
// with its page: the same shape the task page seeds.
type DesignLibraryTask = sessions.TaskLink

// designTasks is every task of the project with its page.
func designTasks(tickets []*domain.Ticket, projectID string) []DesignLibraryTask {
	out := make([]DesignLibraryTask, 0, len(tickets))
	for _, tk := range tickets {
		out = append(out, DesignLibraryTask{ID: tk.ID, Title: tk.Title, Href: Href(projectID, tk.ID)})
	}
	return out
}

// designArtifacts lists artifacts for a seed: [] without a reader.
func (h Handler) designArtifacts(ctx context.Context, f ports.ArtifactFilter) ([]*domain.Artifact, error) {
	if h.Artifacts == nil {
		return []*domain.Artifact{}, nil
	}
	list, err := h.Artifacts.ListArtifacts(ctx, f)
	if list == nil {
		list = []*domain.Artifact{}
	}
	return list, err
}

// ProjectDesign serves GET /projects/{project}/design: the artifacts moved to
// project level, previewed as on the Design tab, each naming its task.
func (h Handler) ProjectDesign(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	tickets, err := h.Tasks.ListTickets(ctx, project.ID)
	if err != nil {
		return err
	}
	assets, err := h.designArtifacts(ctx, ports.ArtifactFilter{ProjectID: project.ID, Scope: domain.ArtifactScopeProject})
	if err != nil {
		return err
	}
	seed := DesignLibrarySeed{ProjectID: project.ID, Tasks: designTasks(tickets, project.ID), Artifacts: assets}
	frame, err := h.Layout(ctx, "Design assets · "+project.Name, project.ID, "")
	if err != nil {
		return err
	}
	view := ProjectDesignView{Frame: frame, ProjectID: project.ID, ProjectName: project.Name, Seed: seed}
	return h.Render(w, r, http.StatusOK, ProjectDesignPage(view))
}
