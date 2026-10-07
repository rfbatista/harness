package tasks

import (
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
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
}

// DesignLibraryTask is a task an asset may come from, with its page.
type DesignLibraryTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href"`
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
	seed := DesignLibrarySeed{ProjectID: project.ID, Tasks: []DesignLibraryTask{}}
	for _, tk := range tickets {
		seed.Tasks = append(seed.Tasks, DesignLibraryTask{ID: tk.ID, Title: tk.Title, Href: Href(project.ID, tk.ID)})
	}
	frame, err := h.Layout(ctx, "Design assets · "+project.Name, project.ID, "")
	if err != nil {
		return err
	}
	view := ProjectDesignView{Frame: frame, ProjectID: project.ID, ProjectName: project.Name, Seed: seed}
	return h.Render(w, r, http.StatusOK, ProjectDesignPage(view))
}
