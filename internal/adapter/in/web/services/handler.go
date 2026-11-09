// Package services is a project's Services page, /projects/{project}/services:
// every service of the project with its live status, started and stopped from
// the page (see "Spec: Web UI — Services page"). The server seeds the list;
// the browser's servicesPage
// (web/src/modules/services/presentation/components/servicesPage.js) renders
// it and follows the project's services feed.
package services

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is a project's Services page.
func Href(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/services"
}

// Lister reads a project's services, as the page's first paint needs them.
type Lister interface {
	ListServices(ctx context.Context, projectID string) (*ports.ServiceList, error)
}

// Handler serves GET /projects/{project}/services.
type Handler struct {
	Projects ports.ProjectReader
	Services Lister
	// Repositories fill the form's repository picker; nil offers none.
	Repositories ports.RepositoryLister
	Layout       shell.Layout
	Render       shell.Renderer
}

// Seed is what servicesPage starts from. List is the answer of list_services,
// or nil when it could not be read: the browser reads it then.
type Seed struct {
	ProjectID string             `json:"project_id"`
	List      *ports.ServiceList `json:"list"`
}

// Repository is a repository the form offers: where a command service runs, or
// whose saved .env an image service exports.
type Repository struct {
	ID   string
	Name string
	// Dir is the repository's folder, the picker's second line.
	Dir string
}

// Page renders the page. A failed services read still renders it, without a
// list.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	seed := Seed{ProjectID: project.ID}
	if list, err := h.Services.ListServices(ctx, project.ID); err != nil {
		slog.Warn("services page: list not read; the browser reads it", "project", project.ID, "err", err)
	} else {
		if list.Services == nil {
			list.Services = []*domain.Service{}
		}
		seed.List = list
	}
	var repos []Repository
	if h.Repositories != nil {
		list, err := h.Repositories.ListRepositories(ctx, project.ID)
		if err != nil {
			slog.Warn("services page: repositories not read; the form offers none", "project", project.ID, "err", err)
		}
		for _, r := range list {
			repos = append(repos, Repository{ID: r.ID, Name: r.Name, Dir: r.RootDir})
		}
	}
	frame, err := h.Layout(ctx, "Services", project.ID, "")
	if err != nil {
		return err
	}
	frame.Rail.Page = shell.PageServices
	return h.Render(w, r, http.StatusOK, Page(frame, project.ID, project.Name, seed, repos))
}
