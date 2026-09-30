// Package projects is the projects bounded context: projects and the git
// repositories they are made of. Deleting a project announces ProjectDeleted;
// what hangs off a project in other contexts (zones, bounded contexts) is
// removed by those contexts.
package projects

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Projects         = (*Service)(nil)
	_ ports.ProjectReader    = (*Service)(nil)
	_ ports.RepositoryReader = (*Service)(nil)
)

// Service implements the projects use cases.
type Service struct {
	projects     ports.ProjectRepository
	repositories ports.RepositoryRepository // nil: repositories unavailable
	events       ports.EventPublisher       // nil: deletes are not announced
}

// NewService returns the projects context.
func NewService(projects ports.ProjectRepository, repositories ports.RepositoryRepository, events ports.EventPublisher) *Service {
	return &Service{projects: projects, repositories: repositories, events: events}
}

var errProjectNotFound = &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}

// ListProjects returns all projects.
func (s *Service) ListProjects(_ context.Context) ([]*domain.Project, error) {
	return s.projects.List(), nil
}

// GetProject returns one project by id, or PROJECT_NOT_FOUND.
func (s *Service) GetProject(_ context.Context, projectID string) (*domain.Project, error) {
	p := s.projects.Get(projectID)
	if p == nil {
		return nil, errProjectNotFound
	}
	return p, nil
}

// CreateProject creates a project with the given name and root directory.
func (s *Service) CreateProject(_ context.Context, name, rootDir string) (*domain.Project, error) {
	return s.projects.Create(name, rootDir)
}

// UpdateProject updates an existing project.
func (s *Service) UpdateProject(_ context.Context, projectID, name, rootDir string) (*domain.Project, error) {
	return s.projects.Update(projectID, name, rootDir)
}

// DeleteProject deletes a project and its repositories, then announces
// ProjectDeleted so other contexts remove what they scoped to it.
func (s *Service) DeleteProject(ctx context.Context, projectID string) error {
	if s.projects.Get(projectID) == nil {
		return errProjectNotFound
	}
	if s.repositories != nil {
		if err := s.repositories.DeleteByProject(projectID); err != nil {
			return err
		}
	}
	if err := s.projects.Delete(projectID); err != nil {
		return err
	}
	if s.events == nil {
		return nil
	}
	return s.events.Publish(ctx, domain.ProjectDeleted{ProjectID: projectID})
}

// AddIgnoredPath adds a path to the project's ignored list (hidden in tree view).
func (s *Service) AddIgnoredPath(_ context.Context, projectID, path string) (*domain.Project, error) {
	return s.projects.AddIgnoredPath(projectID, path)
}

// RemoveIgnoredPath removes a path from the project's ignored list.
func (s *Service) RemoveIgnoredPath(_ context.Context, projectID, path string) (*domain.Project, error) {
	return s.projects.RemoveIgnoredPath(projectID, path)
}

// --- Repositories ---

var errNoRepositories = &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}

// ListRepositories returns all git repositories for the given project.
func (s *Service) ListRepositories(_ context.Context, projectID string) ([]*domain.Repository, error) {
	if s.repositories == nil {
		return nil, nil
	}
	return s.repositories.ListByProject(projectID), nil
}

// GetRepository returns one repository by id, or REPOSITORY_NOT_FOUND.
func (s *Service) GetRepository(_ context.Context, id string) (*domain.Repository, error) {
	var r *domain.Repository
	if s.repositories != nil {
		r = s.repositories.Get(id)
	}
	if r == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return r, nil
}

// CreateRepository creates a git repository scoped to a project.
func (s *Service) CreateRepository(_ context.Context, projectID, name, description, url, rootDir string) (*domain.Repository, error) {
	if s.repositories == nil {
		return nil, errNoRepositories
	}
	if s.projects.Get(projectID) == nil {
		return nil, errProjectNotFound
	}
	return s.repositories.Create(projectID, name, description, url, rootDir)
}

// UpdateRepository updates an existing git repository.
func (s *Service) UpdateRepository(_ context.Context, id, name, description, url, rootDir string) (*domain.Repository, error) {
	if s.repositories == nil {
		return nil, errNoRepositories
	}
	return s.repositories.Update(id, name, description, url, rootDir)
}

// DeleteRepository deletes a git repository by id.
func (s *Service) DeleteRepository(_ context.Context, id string) error {
	if s.repositories == nil {
		return errNoRepositories
	}
	return s.repositories.Delete(id)
}

// AddRepositoryIgnoredPath adds a path to a repository's ignored list.
func (s *Service) AddRepositoryIgnoredPath(_ context.Context, repositoryID, path string) (*domain.Repository, error) {
	if s.repositories == nil {
		return nil, errNoRepositories
	}
	return s.repositories.AddIgnoredPath(repositoryID, path)
}

// RemoveRepositoryIgnoredPath removes a path from a repository's ignored list.
func (s *Service) RemoveRepositoryIgnoredPath(_ context.Context, repositoryID, path string) (*domain.Repository, error) {
	if s.repositories == nil {
		return nil, errNoRepositories
	}
	return s.repositories.RemoveIgnoredPath(repositoryID, path)
}
