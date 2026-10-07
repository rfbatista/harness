package ports

import (
	"context"
	"time"

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
	ProjectSummaries
	RepositoryCatalog
}

// ProjectSummary is one project at a glance: what the projects screen lists.
// Counts are a snapshot at answer time.
type ProjectSummary struct {
	Project         *domain.Project
	RepositoryCount int
	// OpenTaskCount counts the project's tasks that are not done.
	OpenTaskCount int
	// RunningSessionCount counts its live sessions (not done, failed or
	// stopped).
	RunningSessionCount int
	// LastActivityAt is the newest task update or session activity; nil when
	// the project has neither.
	LastActivityAt *time.Time
}

// ProjectSummaries lists every project at a glance, in one call. It is
// network-safe: tui-client implements it over HTTP.
type ProjectSummaries interface {
	// ListProjectSummaries is sorted by name (case-insensitive), then id;
	// never nil.
	ListProjectSummaries(ctx context.Context) ([]ProjectSummary, error)
}

// ProjectCatalogChange is one change to the set of projects: a project
// created or updated (its ignored paths included), or deleted.
type ProjectCatalogChange struct {
	Project *domain.Project
	Deleted bool
}

// ProjectCatalogFeed follows the set of projects as it changes.
type ProjectCatalogFeed interface {
	// FollowProjects delivers each change, in the order the writes happened,
	// until ctx ends. The channel closes when ctx ends or when the follower
	// fell behind; the client then reloads and follows again.
	FollowProjects(ctx context.Context) (<-chan ProjectCatalogChange, error)
}

// TaskActivity is a project's tasks at a glance.
type TaskActivity struct {
	// OpenCount counts the tasks that are not done.
	OpenCount int
	// LastUpdatedAt is the newest task update.
	LastUpdatedAt time.Time
}

// TaskActivityReader is what the projects context reads about tasks.
// Planning implements it.
type TaskActivityReader interface {
	// TaskActivityByProject answers for every project that has tasks, keyed
	// by project id.
	TaskActivityByProject(ctx context.Context) (map[string]TaskActivity, error)
}

// SessionActivity is a project's sessions at a glance.
type SessionActivity struct {
	// LiveCount counts the sessions that are not done, failed or stopped.
	LiveCount int
	// LastActivityAt is the newest session activity.
	LastActivityAt time.Time
}

// SessionActivityReader is what the projects context reads about sessions.
// Orchestration implements it.
type SessionActivityReader interface {
	// SessionActivityByProject answers for every project that has sessions,
	// keyed by project id.
	SessionActivityByProject(ctx context.Context) (map[string]SessionActivity, error)
	// LiveProjectSessions names the project's live sessions.
	LiveProjectSessions(ctx context.Context, projectID string) ([]domain.ProjectSessionRef, error)
}
