// Package productowner is a project's product owner page, /projects/{project}/product-owner
// and /projects/{project}/product-owner/threads/{thread}: the thread list beside the
// conversation (ADR 0115). The page is one React island that the browser draws, so this
// handler only answers the page's frame and the island's props; the threads themselves come
// from the product owner's API.
package productowner

import (
	"encoding/json"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// Href is a project's product owner page.
func Href(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/product-owner"
}

// ThreadHref is a thread's own page.
func ThreadHref(projectID, threadID string) string {
	return Href(projectID) + "/threads/" + url.PathEscape(threadID)
}

// Handler serves the product owner's pages.
type Handler struct {
	Projects ports.ProjectReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Props are the island's props: the project, and the thread the address names ("" on the page
// itself). Whether that thread belongs to the project is the product owner API's to say: the
// island shows its answer.
type Props struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	ThreadID    string `json:"thread_id"`
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
	frame, err := h.Layout(ctx, "Product owner", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageProductOwner
	return h.Render(w, r, http.StatusOK, Page(frame, Props{ProjectID: project.ID, ProjectName: project.Name, ThreadID: r.PathValue("thread")}))
}
