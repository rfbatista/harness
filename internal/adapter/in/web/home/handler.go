package home

import (
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// Handler serves GET /.
type Handler struct {
	Projects ports.ProjectReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Index opens the first project by name; with no project, it explains how to
// create one.
func (h Handler) Index(w http.ResponseWriter, r *http.Request) error {
	projects, err := h.Projects.ListProjects(r.Context())
	if err != nil {
		return err
	}
	if len(projects) > 0 {
		http.Redirect(w, r, "/projects/"+url.PathEscape(firstByName(projects).ID), http.StatusFound)
		return nil
	}
	frame, err := h.Layout(r.Context(), "Welcome", "", "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, NoProjects(frame))
}
