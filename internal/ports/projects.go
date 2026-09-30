package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// Driving ports of the projects context: projects and their git
// repositories. *projects.Service satisfies them.
//
// They are network-safe: tui-client implements them over HTTP, so every
// method takes a context and reports failure — PROJECT_NOT_FOUND,
// REPOSITORY_NOT_FOUND included — as an error.

// ProjectCatalog manages projects.
type ProjectCatalog interface {
	ListProjects(ctx context.Context) ([]*domain.Project, error)
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
	CreateProject(ctx context.Context, name, rootDir string) (*domain.Project, error)
	UpdateProject(ctx context.Context, projectID, name, rootDir string) (*domain.Project, error)
	DeleteProject(ctx context.Context, projectID string) error
	AddIgnoredPath(ctx context.Context, projectID, path string) (*domain.Project, error)
	RemoveIgnoredPath(ctx context.Context, projectID, path string) (*domain.Project, error)
}

// RepositoryCatalog manages the git repositories of a project.
type RepositoryCatalog interface {
	ListRepositories(ctx context.Context, projectID string) ([]*domain.Repository, error)
	GetRepository(ctx context.Context, id string) (*domain.Repository, error)
	CreateRepository(ctx context.Context, projectID, name, description, url, rootDir string) (*domain.Repository, error)
	UpdateRepository(ctx context.Context, id, name, description, url, rootDir string) (*domain.Repository, error)
	DeleteRepository(ctx context.Context, id string) error
	AddRepositoryIgnoredPath(ctx context.Context, repositoryID, path string) (*domain.Repository, error)
	RemoveRepositoryIgnoredPath(ctx context.Context, repositoryID, path string) (*domain.Repository, error)
}

// ProjectReader is what other contexts read about projects.
type ProjectReader interface {
	GetProject(ctx context.Context, projectID string) (*domain.Project, error)
	ListProjects(ctx context.Context) ([]*domain.Project, error)
}

// RepositoryReader is what other contexts read about repositories.
type RepositoryReader interface {
	GetRepository(ctx context.Context, id string) (*domain.Repository, error)
}

// Projects is the whole projects context.
type Projects interface {
	ProjectCatalog
	RepositoryCatalog
}
