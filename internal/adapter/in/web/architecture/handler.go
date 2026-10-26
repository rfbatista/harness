// Package architecture serves a repository's Architecture page: the shell, its
// tabs and the deep links to them. The record itself is loaded and written by
// the browser through /api (web/src/modules/architecture); this page only
// renders what is known without it.
package architecture

import (
	"net/http"
	"net/url"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Handler serves GET /projects/{project}/repositories/{repository}/architecture
// and its tab deep links.
type Handler struct {
	Projects     ports.ProjectReader
	Repositories ports.RepositoryLister
	Layout       shell.Layout
	Render       shell.Renderer
}

// Href is a repository's Architecture page; tab is "" for its first tab, or a
// deep link ("layers", "rules", "file", "diagram", …).
func Href(projectID, repositoryID, tab string) string {
	href := "/projects/" + url.PathEscape(projectID) + "/repositories/" + url.PathEscape(repositoryID) + "/architecture"
	if tab != "" {
		href += "/" + tab
	}
	return href
}

// Seed is what architecturePage starts from.
type Seed struct {
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id"`
	// Base is the page's own path without a tab; tab links are Base + "/" + slug.
	Base string `json:"base"`
	// Tab is the deep link that was opened, "" for the first tab.
	Tab string `json:"tab"`
}

// View is what the page renders.
type View struct {
	Frame          shell.Frame
	ProjectID      string
	ProjectName    string
	RepositoryName string
	SettingsHref   string
	Seed           Seed
}

// Page serves the page; tab is the deep link this route stands for.
func (h Handler) Page(tab string) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
		if err != nil {
			return err
		}
		repos, err := h.Repositories.ListRepositories(ctx, project.ID)
		if err != nil {
			return err
		}
		var repo *domain.Repository
		for _, x := range repos {
			if x.ID == r.PathValue("repository") {
				repo = x
			}
		}
		if repo == nil {
			// A repository of another project is not found, as if it did not exist.
			return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found in this project"}
		}
		frame, err := h.Layout(ctx, "Architecture", project.ID, "")
		if err != nil {
			return err
		}
		frame.Live = true // the page follows the project feed
		name := repo.Name
		if name == "" {
			name = repo.RootDir
		}
		return h.Render(w, r, http.StatusOK, PageView(View{
			Frame:          frame,
			ProjectID:      project.ID,
			ProjectName:    project.Name,
			RepositoryName: name,
			SettingsHref:   "/projects/" + url.PathEscape(project.ID) + "/repositories",
			Seed: Seed{
				ProjectID:    project.ID,
				RepositoryID: repo.ID,
				Base:         Href(project.ID, repo.ID, ""),
				Tab:          tab,
			},
		}))
	}
}
