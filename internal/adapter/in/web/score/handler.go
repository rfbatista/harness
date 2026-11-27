// Package score is a project's Score page, /projects/{project}/score: the eval bench's scorecard beside a baseline, its cases and
// its runs (ADR 0015). The page is one React island that the browser draws from the bench API, so this handler only answers the
// page's frame and the island's props.
package score

import (
	"encoding/json"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// Href is a project's Score page.
func Href(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/score"
}

// Handler serves GET /projects/{project}/score.
type Handler struct {
	Projects ports.ProjectReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Props are the island's props: the project.
type Props struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

// JSON is the props as a script can carry them: json.Marshal escapes what could close it.
func (p Props) JSON() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// Page renders the page, or the fragment of it that replaces <main> within the project.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, "Score", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageScore
	return h.Render(w, r, http.StatusOK, Page(frame, Props{ProjectID: project.ID, ProjectName: project.Name}))
}
