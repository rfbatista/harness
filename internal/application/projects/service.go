// Package projects is the projects bounded context: projects and the git
// repositories they are made of. Deleting a project announces ProjectDeleted;
// what hangs off a project in other contexts (zones, bounded contexts) is
// removed by those contexts.
package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Projects         = (*Service)(nil)
	_ ports.ProjectReader    = (*Service)(nil)
	_ ports.RepositoryReader = (*Service)(nil)

	_ ports.RepositoryDiscovery   = (*Service)(nil)
	_ ports.RepositoryEnv         = (*Service)(nil)
	_ ports.RepositoryRunCommands = (*Service)(nil)

	_ ports.ProjectCatalogFeed = (*Service)(nil)
)

// Service implements the projects use cases.
type Service struct {
	projects     ports.ProjectRepository
	repositories ports.RepositoryRepository  // nil: repositories unavailable
	events       ports.EventPublisher        // nil: deletes are not announced
	finder       ports.RepositoryFinder      // nil: discovery unavailable
	envFiles     ports.EnvFileRepository     // nil: env files unavailable
	envIO        ports.EnvFileIO             // nil: importing from checkouts unavailable
	runCommands  ports.RunCommandRepository  // nil: saved run commands unavailable
	tasks        ports.TaskActivityReader    // nil: summaries count no tasks
	sessions     ports.SessionActivityReader // nil: no sessions counted, deletes not guarded

	// mu serialises project writes, so a name is checked and taken at once
	// and the feed announces changes in the order they were made.
	mu   sync.Mutex
	feed *feed
}

// UseActivity lets the context read the tasks and sessions of its projects:
// summaries count them, and a project with live sessions is not deleted.
// Planning and orchestration are built after the catalog, hence the setter.
func (s *Service) UseActivity(tasks ports.TaskActivityReader, sessions ports.SessionActivityReader) {
	s.tasks, s.sessions = tasks, sessions
}

// UseRunCommands lets the context keep repositories' saved run commands.
func (s *Service) UseRunCommands(store ports.RunCommandRepository) { s.runCommands = store }

// UseEnvFiles lets the context keep repositories' env files, and import them
// from checkouts when io is set.
func (s *Service) UseEnvFiles(store ports.EnvFileRepository, io ports.EnvFileIO) {
	s.envFiles, s.envIO = store, io
}

// UseFinder lets the context look for git checkouts on disk.
func (s *Service) UseFinder(f ports.RepositoryFinder) { s.finder = f }

// findDepth is how far below a directory checkouts are looked for: deep
// enough for src/<group>/<repo>, shallow enough to stay quick on a home dir.
const findDepth = 3

// FindRepositories lists the git checkouts inside rootDir on this machine,
// for a client choosing which to add. "~/" is expanded; the directory must
// be absolute and exist.
func (s *Service) FindRepositories(_ context.Context, rootDir string) (string, []ports.FoundRepository, error) {
	if s.finder == nil {
		return "", nil, &domain.StructuredError{Code: "INTERNAL", Message: "repository discovery is not configured"}
	}
	root, err := resolveDir(rootDir)
	if err != nil {
		return "", nil, err
	}
	found, err := s.finder.FindRepositories(root, findDepth)
	if err != nil {
		return "", nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "cannot read " + root + ": " + err.Error()}
	}
	if found == nil {
		found = []ports.FoundRepository{}
	}
	return root, found, nil
}

// resolveDir expands a leading "~/" and checks the result is an existing
// absolute directory.
func resolveDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", &domain.StructuredError{Code: "INVALID_ROOT", Message: "cannot resolve ~: " + err.Error()}
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		return "", &domain.StructuredError{Code: "INVALID_ROOT", Message: "the directory must be an absolute path"}
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", &domain.StructuredError{Code: "INVALID_ROOT", Message: dir + " is not a directory on the server's machine"}
	}
	return filepath.Clean(dir), nil
}

// NewService returns the projects context.
func NewService(projects ports.ProjectRepository, repositories ports.RepositoryRepository, events ports.EventPublisher) *Service {
	return &Service{projects: projects, repositories: repositories, events: events, feed: newFeed()}
}

var (
	errProjectNotFound  = &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	errProjectIDMissing = &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
)

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

// CreateProject creates a project. The name is trimmed and must be unused
// (case-insensitive); the root must be an existing directory, "~" expanded,
// and is stored cleaned.
func (s *Service) CreateProject(_ context.Context, name, rootDir string) (*domain.Project, error) {
	name, err := domain.CleanProjectName(name)
	if err != nil {
		return nil, err
	}
	root, err := resolveProjectRoot(rootDir)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.nameFree(name, ""); err != nil {
		return nil, err
	}
	return s.announced(s.projects.Create(name, root))
}

// UpdateProject renames and/or re-points a project; an empty field keeps its
// value. The create rules apply only to a field that changes, so a project
// stored before them (a colliding name, a root that is gone) can still have
// its other field edited.
func (s *Service) UpdateProject(_ context.Context, projectID, name, rootDir string) (*domain.Project, error) {
	if projectID == "" {
		return nil, errProjectIDMissing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.projects.Get(projectID)
	if cur == nil {
		return nil, errProjectNotFound
	}
	name = strings.TrimSpace(name)
	if name != "" && !domain.SameProjectName(name, cur.Name) {
		if err := s.nameFree(name, projectID); err != nil {
			return nil, err
		}
	}
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == cur.RootDir {
		rootDir = ""
	}
	if rootDir != "" {
		root, err := resolveProjectRoot(rootDir)
		if err != nil {
			return nil, err
		}
		rootDir = root
	}
	return s.announced(s.projects.Update(projectID, name, rootDir))
}

// nameFree is PROJECT_NAME_TAKEN when a project other than exceptID already
// has name.
func (s *Service) nameFree(name, exceptID string) error {
	for _, p := range s.projects.List() {
		if p.ID != exceptID && domain.SameProjectName(p.Name, name) {
			return &domain.StructuredError{Code: "PROJECT_NAME_TAKEN", Message: "another project is already called " + p.Name}
		}
	}
	return nil
}

// resolveProjectRoot is resolveDir answering PROJECT_ROOT_INVALID.
func resolveProjectRoot(dir string) (string, error) {
	root, err := resolveDir(dir)
	if err != nil {
		var se *domain.StructuredError
		if errors.As(err, &se) {
			return "", &domain.StructuredError{Code: "PROJECT_ROOT_INVALID", Message: se.Message}
		}
		return "", err
	}
	return root, nil
}

// DeleteProject deletes a project and its repositories, then announces
// ProjectDeleted so other contexts remove what they scoped to it. A project
// with live sessions is not deleted: PROJECT_HAS_RUNNING_SESSIONS names them.
func (s *Service) DeleteProject(ctx context.Context, projectID string) error {
	if projectID == "" {
		return errProjectIDMissing
	}
	if err := s.deleteProject(ctx, projectID); err != nil {
		return err
	}
	if s.events == nil {
		return nil
	}
	// Published outside the write lock: subscribers run synchronously and may
	// call back into this context.
	return s.events.Publish(ctx, domain.ProjectDeleted{ProjectID: projectID})
}

// deleteProject removes the project and its repositories, under the write
// lock, unless sessions of it are live.
func (s *Service) deleteProject(ctx context.Context, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.projects.Get(projectID)
	if p == nil {
		return errProjectNotFound
	}
	if err := s.refuseRunningSessions(ctx, p); err != nil {
		return err
	}
	if s.repositories != nil {
		for _, r := range s.repositories.ListByProject(projectID) {
			if err := s.deleteRepositoryConfig(r.ID); err != nil {
				return err
			}
		}
		if err := s.repositories.DeleteByProject(projectID); err != nil {
			return err
		}
	}
	if err := s.projects.Delete(projectID); err != nil {
		return err
	}
	s.feed.announce(ports.ProjectCatalogChange{Project: &domain.Project{ID: projectID}, Deleted: true})
	return nil
}

// refuseRunningSessions is PROJECT_HAS_RUNNING_SESSIONS, naming them, when p
// has live sessions. The message carries the code and the sessions because
// MCP clients see only the message.
func (s *Service) refuseRunningSessions(ctx context.Context, p *domain.Project) error {
	if s.sessions == nil {
		return nil
	}
	live, err := s.sessions.LiveProjectSessions(ctx, p.ID)
	if err != nil || len(live) == 0 {
		return err
	}
	names := make([]string, len(live))
	for i, r := range live {
		names[i] = r.ID
		if r.TicketID != "" {
			names[i] += " on task " + r.TicketID
		}
		if r.Agent != "" {
			names[i] += " by " + r.Agent
		}
	}
	noun := "sessions"
	if len(live) == 1 {
		noun = "session"
	}
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{
			Code: "PROJECT_HAS_RUNNING_SESSIONS",
			Message: fmt.Sprintf("PROJECT_HAS_RUNNING_SESSIONS: project %q has %d running %s (%s); stop them first",
				p.Name, len(live), noun, strings.Join(names, ", ")),
		},
		Details: map[string]any{"sessions": live},
	}
}

// FollowProjects delivers each project created, updated or deleted until ctx
// ends.
func (s *Service) FollowProjects(ctx context.Context) (<-chan ports.ProjectCatalogChange, error) {
	return s.feed.follow(ctx), nil
}

// ListProjectSummaries returns every project with its repository, open task
// and live session counts and its last activity, sorted by name.
func (s *Service) ListProjectSummaries(ctx context.Context) ([]ports.ProjectSummary, error) {
	repos := map[string]int{}
	if s.repositories != nil {
		var err error
		if repos, err = s.repositories.CountByProject(); err != nil {
			return nil, err
		}
	}
	var tasks map[string]ports.TaskActivity
	if s.tasks != nil {
		var err error
		if tasks, err = s.tasks.TaskActivityByProject(ctx); err != nil {
			return nil, err
		}
	}
	var sessions map[string]ports.SessionActivity
	if s.sessions != nil {
		var err error
		if sessions, err = s.sessions.SessionActivityByProject(ctx); err != nil {
			return nil, err
		}
	}
	list := s.projects.List()
	out := make([]ports.ProjectSummary, 0, len(list))
	for _, p := range list {
		t, se := tasks[p.ID], sessions[p.ID]
		out = append(out, ports.ProjectSummary{
			Project:             p,
			RepositoryCount:     repos[p.ID],
			OpenTaskCount:       t.OpenCount,
			RunningSessionCount: se.LiveCount,
			LastActivityAt:      newest(t.LastUpdatedAt, se.LastActivityAt),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Project.Name), strings.ToLower(out[j].Project.Name)
		if a != b {
			return a < b
		}
		return out[i].Project.ID < out[j].Project.ID
	})
	return out, nil
}

// newest is the later of two times, nil when both are zero.
func newest(a, b time.Time) *time.Time {
	if b.After(a) {
		a = b
	}
	if a.IsZero() {
		return nil
	}
	return &a
}

// AddIgnoredPath adds a path to the project's ignored list (hidden in tree view).
func (s *Service) AddIgnoredPath(_ context.Context, projectID, path string) (*domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.announced(s.projects.AddIgnoredPath(projectID, path))
}

// RemoveIgnoredPath removes a path from the project's ignored list.
func (s *Service) RemoveIgnoredPath(_ context.Context, projectID, path string) (*domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.announced(s.projects.RemoveIgnoredPath(projectID, path))
}

// announced puts a written project on the feed and passes the write's result
// through.
func (s *Service) announced(p *domain.Project, err error) (*domain.Project, error) {
	if err != nil {
		return nil, err
	}
	s.feed.announce(ports.ProjectCatalogChange{Project: p})
	return p, nil
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

// DeleteRepository deletes a git repository by id, with its env files.
func (s *Service) DeleteRepository(_ context.Context, id string) error {
	if s.repositories == nil {
		return errNoRepositories
	}
	if err := s.repositories.Delete(id); err != nil {
		return err
	}
	return s.deleteRepositoryConfig(id)
}

// deleteRepositoryConfig removes what the harness keeps for a repository:
// its env files and run commands.
func (s *Service) deleteRepositoryConfig(repositoryID string) error {
	if s.envFiles != nil {
		if err := s.envFiles.DeleteByRepository(repositoryID); err != nil {
			return err
		}
	}
	if s.runCommands != nil {
		if err := s.runCommands.DeleteByRepository(repositoryID); err != nil {
			return err
		}
	}
	return nil
}

// --- Run commands ---

func (s *Service) runCommandRepository(repositoryID string) error {
	if s.runCommands == nil || s.repositories == nil {
		return &domain.StructuredError{Code: "INTERNAL", Message: "run command store not configured"}
	}
	if s.repositories.Get(repositoryID) == nil {
		return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return nil
}

// ListRunCommands returns the repository's saved run commands, by name.
func (s *Service) ListRunCommands(_ context.Context, repositoryID string) ([]*domain.RunCommand, error) {
	if err := s.runCommandRepository(repositoryID); err != nil {
		return nil, err
	}
	return s.runCommands.List(repositoryID), nil
}

// SaveRunCommand creates or replaces the repository's command called name.
func (s *Service) SaveRunCommand(_ context.Context, repositoryID, name, command string) (*domain.RunCommand, error) {
	if err := s.runCommandRepository(repositoryID); err != nil {
		return nil, err
	}
	name, command, err := domain.CleanRunCommand(name, command)
	if err != nil {
		return nil, err
	}
	return s.runCommands.Put(&domain.RunCommand{RepositoryID: repositoryID, Name: name, Command: command})
}

// DeleteRunCommand removes the repository's command called name.
func (s *Service) DeleteRunCommand(_ context.Context, repositoryID, name string) error {
	if err := s.runCommandRepository(repositoryID); err != nil {
		return err
	}
	return s.runCommands.Delete(repositoryID, strings.TrimSpace(name))
}

// --- Env files ---

var errNoEnvFiles = &domain.StructuredError{Code: "INTERNAL", Message: "env file store not configured"}

// envRepository is the repository whose env files are asked for.
func (s *Service) envRepository(repositoryID string) (*domain.Repository, error) {
	if s.envFiles == nil || s.repositories == nil {
		return nil, errNoEnvFiles
	}
	r := s.repositories.Get(repositoryID)
	if r == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return r, nil
}

// ListEnvFiles returns the env files the harness writes into the repository's
// session worktrees.
func (s *Service) ListEnvFiles(_ context.Context, repositoryID string) ([]*domain.EnvFile, error) {
	if _, err := s.envRepository(repositoryID); err != nil {
		return nil, err
	}
	return s.envFiles.List(repositoryID), nil
}

// SaveEnvFile creates or replaces one env file of the repository.
func (s *Service) SaveEnvFile(_ context.Context, repositoryID, path, content string) (*domain.EnvFile, error) {
	if _, err := s.envRepository(repositoryID); err != nil {
		return nil, err
	}
	clean, err := domain.CleanEnvFilePath(path)
	if err != nil {
		return nil, err
	}
	if len(content) > domain.MaxEnvFileSize {
		return nil, &domain.StructuredError{Code: "ENV_FILE_TOO_LARGE", Message: "an env file holds at most 256 KB"}
	}
	return s.envFiles.Put(&domain.EnvFile{RepositoryID: repositoryID, Path: clean, Content: content})
}

// DeleteEnvFile removes one env file; sessions started afterwards no longer
// get it (worktrees already written keep their copy).
func (s *Service) DeleteEnvFile(_ context.Context, repositoryID, path string) error {
	if _, err := s.envRepository(repositoryID); err != nil {
		return err
	}
	clean, err := domain.CleanEnvFilePath(path)
	if err != nil {
		return err
	}
	return s.envFiles.Delete(repositoryID, clean)
}

// ImportEnvFile saves the file at path as it is now in the repository's
// checkout, the usual way to start: the developer's own .env.
func (s *Service) ImportEnvFile(ctx context.Context, repositoryID, path string) (*domain.EnvFile, error) {
	repo, err := s.envRepository(repositoryID)
	if err != nil {
		return nil, err
	}
	if s.envIO == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "reading checkouts is not configured"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no local checkout to import from"}
	}
	clean, err := domain.CleanEnvFilePath(path)
	if err != nil {
		return nil, err
	}
	b, err := s.envIO.Read(domain.ExpandUserPath(repo.RootDir), clean)
	if err != nil {
		return nil, err
	}
	return s.SaveEnvFile(ctx, repositoryID, clean, string(b))
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
