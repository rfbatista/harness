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

// FoundRepository is a git checkout found on disk, not yet (necessarily)
// recorded as a repository.
type FoundRepository struct {
	// Path is the checkout's absolute directory.
	Path string `json:"path"`
	// Name is the directory's name.
	Name string `json:"name"`
	// Remote is its origin's URL; empty for a local-only checkout.
	Remote string `json:"remote,omitempty"`
}

// RepositoryFinder finds the git checkouts inside a directory: the directory
// itself when it is one, and those below it, depth levels down at most. It
// does not look inside a checkout it found.
type RepositoryFinder interface {
	FindRepositories(root string, depth int) ([]FoundRepository, error)
}

// RepositoryDiscovery is what a client asks before adding repositories: the
// checkouts inside a directory on the server's machine. root is the
// directory as the server resolved it (~ expanded); a missing or relative
// directory is INVALID_ROOT.
type RepositoryDiscovery interface {
	FindRepositories(ctx context.Context, rootDir string) (root string, found []FoundRepository, err error)
}

// EnvFileRepository stores the env files of repositories (driven).
type EnvFileRepository interface {
	List(repositoryID string) []*domain.EnvFile
	// Put creates or replaces the file at f.RepositoryID + f.Path.
	Put(f *domain.EnvFile) (*domain.EnvFile, error)
	// Delete removes one file, or ENV_FILE_NOT_FOUND.
	Delete(repositoryID, path string) error
	DeleteByRepository(repositoryID string) error
}

// EnvFileIO moves env files between the harness and git checkouts (driven).
type EnvFileIO interface {
	// Read returns the file at rel inside the checkout at root.
	Read(root, rel string) ([]byte, error)
	// Write writes files into the worktree at root, owner-readable only, and
	// makes sure git ignores each, so a session cannot commit them.
	Write(root string, files []*domain.EnvFile) error
}

// EnvFileLister reads a repository's env files: what a page showing them
// needs. RepositoryEnv embeds it.
type EnvFileLister interface {
	ListEnvFiles(ctx context.Context, repositoryID string) ([]*domain.EnvFile, error)
}

// RepositoryEnv manages the env files the harness writes into a repository's
// session worktrees. Paths are relative to the checkout; a bad one is
// INVALID_PATH, an unknown repository REPOSITORY_NOT_FOUND.
type RepositoryEnv interface {
	EnvFileLister
	SaveEnvFile(ctx context.Context, repositoryID, path, content string) (*domain.EnvFile, error)
	DeleteEnvFile(ctx context.Context, repositoryID, path string) error
	// ImportEnvFile reads the file at path from the repository's checkout and
	// saves it; ENV_FILE_NOT_FOUND when the checkout has none.
	ImportEnvFile(ctx context.Context, repositoryID, path string) (*domain.EnvFile, error)
}

// RunCommandRepository stores repositories' saved run commands (driven).
type RunCommandRepository interface {
	List(repositoryID string) []*domain.RunCommand
	// Put creates or replaces the command named c.Name.
	Put(c *domain.RunCommand) (*domain.RunCommand, error)
	// Delete removes one, or RUN_COMMAND_NOT_FOUND.
	Delete(repositoryID, name string) error
	DeleteByRepository(repositoryID string) error
}

// RunCommandLister reads a repository's saved run commands: what running one
// needs. RepositoryRunCommands embeds it.
type RunCommandLister interface {
	ListRunCommands(ctx context.Context, repositoryID string) ([]*domain.RunCommand, error)
}

// RepositoryRunCommands manages the named commands that run a repository's
// application. A bad name is INVALID_NAME; an unknown repository
// REPOSITORY_NOT_FOUND.
type RepositoryRunCommands interface {
	RunCommandLister
	SaveRunCommand(ctx context.Context, repositoryID, name, command string) (*domain.RunCommand, error)
	DeleteRunCommand(ctx context.Context, repositoryID, name string) error
}

// RepositoryLister lists a project's repositories: what a page offering
// them to pick from needs. RepositoryCatalog embeds it.
type RepositoryLister interface {
	ListRepositories(ctx context.Context, projectID string) ([]*domain.Repository, error)
}

// RepositoryCatalog manages the git repositories of a project.
type RepositoryCatalog interface {
	RepositoryLister
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
