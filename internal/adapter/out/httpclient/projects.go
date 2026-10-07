package httpclient

import (
	"context"
	"net/url"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.Projects = (*Projects)(nil)

// Projects is ports.Projects over the HTTP API.
type Projects struct{ c *Client }

// NewProjects returns the projects adapter over c.
func NewProjects(c *Client) *Projects { return &Projects{c: c} }

type projectOut struct {
	Project *domain.Project `json:"project"`
}

func (p *Projects) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	var out struct {
		Projects []*domain.Project `json:"projects"`
	}
	return out.Projects, p.c.get(ctx, "/api/list_projects", nil, &out)
}

func (p *Projects) ListProjectSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	var out struct {
		Summaries []struct {
			Project             *domain.Project `json:"project"`
			RepositoryCount     int             `json:"repository_count"`
			OpenTaskCount       int             `json:"open_task_count"`
			RunningSessionCount int             `json:"running_session_count"`
			LastActivityAt      *time.Time      `json:"last_activity_at"`
		} `json:"summaries"`
	}
	if err := p.c.get(ctx, "/api/list_project_summaries", nil, &out); err != nil {
		return nil, err
	}
	list := make([]ports.ProjectSummary, len(out.Summaries))
	for i, s := range out.Summaries {
		list[i] = ports.ProjectSummary{
			Project:             s.Project,
			RepositoryCount:     s.RepositoryCount,
			OpenTaskCount:       s.OpenTaskCount,
			RunningSessionCount: s.RunningSessionCount,
			LastActivityAt:      s.LastActivityAt,
		}
	}
	return list, nil
}

func (p *Projects) GetProject(ctx context.Context, projectID string) (*domain.Project, error) {
	var out projectOut
	return out.Project, p.c.get(ctx, "/api/get_project", url.Values{"project_id": {projectID}}, &out)
}

func (p *Projects) CreateProject(ctx context.Context, name, rootDir string) (*domain.Project, error) {
	var out projectOut
	err := p.c.post(ctx, "/api/create_project", map[string]string{"name": name, "root_dir": rootDir}, &out)
	return out.Project, err
}

func (p *Projects) UpdateProject(ctx context.Context, projectID, name, rootDir string) (*domain.Project, error) {
	var out projectOut
	err := p.c.post(ctx, "/api/update_project", map[string]string{
		"project_id": projectID, "name": name, "root_dir": rootDir,
	}, &out)
	return out.Project, err
}

func (p *Projects) DeleteProject(ctx context.Context, projectID string) error {
	return p.c.post(ctx, "/api/delete_project", map[string]string{"project_id": projectID}, nil)
}

func (p *Projects) AddIgnoredPath(ctx context.Context, projectID, path string) (*domain.Project, error) {
	var out projectOut
	err := p.c.post(ctx, "/api/add_ignored_path", map[string]string{"project_id": projectID, "path": path}, &out)
	return out.Project, err
}

func (p *Projects) RemoveIgnoredPath(ctx context.Context, projectID, path string) (*domain.Project, error) {
	var out projectOut
	err := p.c.post(ctx, "/api/remove_ignored_path", map[string]string{"project_id": projectID, "path": path}, &out)
	return out.Project, err
}

type repositoryOut struct {
	Repository *domain.Repository `json:"repository"`
}

func (p *Projects) ListRepositories(ctx context.Context, projectID string) ([]*domain.Repository, error) {
	var out struct {
		Repositories []*domain.Repository `json:"repositories"`
	}
	return out.Repositories, p.c.get(ctx, "/api/list_repositories", url.Values{"project_id": {projectID}}, &out)
}

func (p *Projects) GetRepository(ctx context.Context, id string) (*domain.Repository, error) {
	var out repositoryOut
	return out.Repository, p.c.get(ctx, "/api/get_repository", url.Values{"repository_id": {id}}, &out)
}

func (p *Projects) CreateRepository(ctx context.Context, projectID, name, description, repoURL, rootDir string) (*domain.Repository, error) {
	var out repositoryOut
	err := p.c.post(ctx, "/api/create_repository", map[string]string{
		"project_id": projectID, "name": name, "description": description, "url": repoURL, "root_dir": rootDir,
	}, &out)
	return out.Repository, err
}

func (p *Projects) UpdateRepository(ctx context.Context, id, name, description, repoURL, rootDir string) (*domain.Repository, error) {
	var out repositoryOut
	err := p.c.post(ctx, "/api/update_repository", map[string]string{
		"repository_id": id, "name": name, "description": description, "url": repoURL, "root_dir": rootDir,
	}, &out)
	return out.Repository, err
}

func (p *Projects) DeleteRepository(ctx context.Context, id string) error {
	return p.c.post(ctx, "/api/delete_repository", map[string]string{"repository_id": id}, nil)
}

func (p *Projects) AddRepositoryIgnoredPath(ctx context.Context, repositoryID, path string) (*domain.Repository, error) {
	var out repositoryOut
	err := p.c.post(ctx, "/api/add_repository_ignored_path", map[string]string{"repository_id": repositoryID, "path": path}, &out)
	return out.Repository, err
}

func (p *Projects) RemoveRepositoryIgnoredPath(ctx context.Context, repositoryID, path string) (*domain.Repository, error) {
	var out repositoryOut
	err := p.c.post(ctx, "/api/remove_repository_ignored_path", map[string]string{"repository_id": repositoryID, "path": path}, &out)
	return out.Repository, err
}
