// Package agentsystem is a project's Agent system page, /projects/{project}/agent-system: where the
// person configures the four layers (Learn, Guard, Review, Score) for the project and overrides them per
// repository (ADR 0011). The page is one React island that the browser draws from the agent-system API,
// so this handler only answers the page's frame and the island's props.
package agentsystem

import (
	"encoding/json"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is a project's Agent system page; repository "" is the project defaults.
func Href(projectID, repositoryID string) string {
	href := "/projects/" + url.PathEscape(projectID) + "/agent-system"
	if repositoryID != "" {
		href += "?repository=" + url.QueryEscape(repositoryID)
	}
	return href
}

// Handler serves GET /projects/{project}/agent-system.
type Handler struct {
	Projects ports.ProjectReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Model is one model the critic's select offers.
type Model struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Models are the models the harness can call (domain.ModelChoices), as the Models page offers them.
func Models() []Model {
	out := make([]Model, 0, len(domain.ModelChoices))
	for _, m := range domain.ModelChoices {
		label := m
		if m == domain.ModelDefault {
			label = "default: the CLI chooses"
		}
		out = append(out, Model{Value: m, Label: label})
	}
	return out
}

// Props are the island's props: the project and the models the critic can use. The scope comes from the
// address, which the island reads.
type Props struct {
	ProjectID   string  `json:"project_id"`
	ProjectName string  `json:"project_name"`
	Models      []Model `json:"models"`
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
	frame, err := h.Layout(ctx, "Agent system", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageAgentSystem
	return h.Render(w, r, http.StatusOK, Page(frame, Props{ProjectID: project.ID, ProjectName: project.Name, Models: Models()}))
}
