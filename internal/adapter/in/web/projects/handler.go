// Package projects serves the pages that shape a project: creating one, and
// its repositories, the git checkouts its sessions run in. Writes go from
// the browser to /api (web/src/modules/projects); these pages only render.
package projects

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Handler serves GET /projects/new, GET /projects/{project}/repositories and
// GET /projects/{project}/repositories/{repository}/env.
type Handler struct {
	Projects     ports.ProjectReader
	Repositories ports.RepositoryLister
	EnvFiles     ports.EnvFileLister
	Layout       shell.Layout
	Render       shell.Renderer
}

// New is the new-project form.
func (h Handler) New(w http.ResponseWriter, r *http.Request) error {
	frame, err := h.Layout(r.Context(), "New project", "", "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, NewProjectPage(frame))
}

// RepositoryList is a project's repositories page.
func (h Handler) RepositoryList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	repos, err := h.Repositories.ListRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, "Repositories", project.ID, "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, RepositoriesPage(NewRepositoriesView(frame, project, repos)))
}

// EnvFileList is a repository's env files page. A repository of another project
// is REPOSITORY_NOT_FOUND, as if it did not exist.
func (h Handler) EnvFileList(w http.ResponseWriter, r *http.Request) error {
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
	for _, rp := range repos {
		if rp.ID == r.PathValue("repository") {
			repo = rp
		}
	}
	if repo == nil {
		return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found in this project"}
	}
	files, err := h.EnvFiles.ListEnvFiles(ctx, repo.ID)
	if err != nil {
		return err
	}
	if files == nil {
		files = []*domain.EnvFile{}
	}
	frame, err := h.Layout(ctx, "Env files", project.ID, "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, EnvFilesPage(EnvFilesView{
		Frame:          frame,
		ProjectID:      project.ID,
		ProjectName:    project.Name,
		RepositoryName: repositoryName(repo),
		RootDir:        repo.RootDir,
		Files:          files,
		Seed:           EnvFilesSeed{RepositoryID: repo.ID, EnvFiles: files},
	}))
}

// EnvFilesView is what the env files page renders.
type EnvFilesView struct {
	Frame                                           shell.Frame
	ProjectID, ProjectName, RepositoryName, RootDir string
	Files                                           []*domain.EnvFile
	Seed                                            EnvFilesSeed
}

// EnvFilesSeed is what projectsEnvFilesPage starts from, in the API's shape.
type EnvFilesSeed struct {
	RepositoryID string            `json:"repository_id"`
	EnvFiles     []*domain.EnvFile `json:"env_files"`
}

// EnvFilesHref is a repository's env files page.
func EnvFilesHref(projectID, repositoryID string) string {
	return RepositoriesHref(projectID) + "/" + url.PathEscape(repositoryID) + "/env"
}

// RepositoriesHref is a project's repositories page.
func RepositoriesHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/repositories"
}

// RepositoriesView is what the repositories page renders.
type RepositoriesView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	Rows        []RepositoryRow
	Seed        Seed
}

// RepositoryRow is one repository as listed.
type RepositoryRow struct{ ID, Name, RootDir, Remote, EnvHref, HistoryHref string }

// Seed is what projectsRepositoriesPage starts from, in the API's shape.
type Seed struct {
	ProjectID string `json:"project_id"`
	// ProjectRoot is where the page looks for checkouts not added yet.
	ProjectRoot  string               `json:"project_root"`
	Repositories []*domain.Repository `json:"repositories"`
}

// NewRepositoriesView builds the page. It reads names and remotes the way
// web/src/modules/projects/presentation/pages/repositoriesPage.js does.
func NewRepositoriesView(frame shell.Frame, project *domain.Project, repos []*domain.Repository) RepositoriesView {
	if repos == nil {
		repos = []*domain.Repository{}
	}
	rows := make([]RepositoryRow, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, RepositoryRow{ID: r.ID, Name: repositoryName(r), RootDir: rootDirLabel(r.RootDir), Remote: remoteLabel(r.URL), EnvHref: EnvFilesHref(project.ID, r.ID), HistoryHref: HistoryHref(project.ID, r.ID)})
	}
	return RepositoriesView{
		Frame:       frame,
		ProjectID:   project.ID,
		ProjectName: project.Name,
		Rows:        rows,
		Seed:        Seed{ProjectID: project.ID, ProjectRoot: project.RootDir, Repositories: repos},
	}
}

func repositoryName(r *domain.Repository) string {
	switch {
	case r.Name != "":
		return r.Name
	case r.RootDir != "":
		return path.Base(strings.TrimRight(r.RootDir, "/"))
	default:
		return r.URL
	}
}

func rootDirLabel(dir string) string {
	if dir == "" {
		return "no local path: sessions cannot run here"
	}
	return dir
}

func remoteLabel(u string) string {
	if strings.HasPrefix(u, "file://") {
		return "local only"
	}
	return u
}

// HistoryHref is a repository's history page (served by package history,
// which this one cannot import: history reads tasks, tasks reads projects).
func HistoryHref(projectID, repositoryID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/repositories/" + url.PathEscape(repositoryID) + "/history"
}
