// Package workspaces provides use-cases for repository workspaces: isolated
// git worktrees materialized on disk and tracked in the database. It is kept
// separate from the catalog contexts so the workspace domain stays isolated.
package workspaces

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var (
	_ ports.Workspaces        = (*Service)(nil)
	_ ports.RepositoryHistory = (*Service)(nil)
)

// defaultWorktreesRoot is where worktrees land when workspaces.root is unset: a
// single folder outside every checkout, so no worktree pollutes a repository.
const defaultWorktreesRoot = "~/.coding-pool/worktrees"

// Service implements workspace use-cases over the outbound ports.
type Service struct {
	workspaces   ports.WorkspaceRepository
	repositories ports.RepositoryRepository
	worktrees    ports.WorktreeManager
	settings     ports.SettingsRepository
	envFiles     ports.EnvFileRepository // nil: worktrees get no env files
	envIO        ports.EnvFileIO
	history      ports.GitHistory // nil: repository history unavailable
}

// UseHistory lets the service read repositories' commit graphs.
func (s *Service) UseHistory(h ports.GitHistory) { s.history = h }

// UseEnvFiles makes every worktree start with its repository's env files.
func (s *Service) UseEnvFiles(store ports.EnvFileRepository, io ports.EnvFileIO) {
	s.envFiles, s.envIO = store, io
}

// NewService returns a workspaces service.
func NewService(
	ws ports.WorkspaceRepository,
	repos ports.RepositoryRepository,
	wt ports.WorktreeManager,
	settings ports.SettingsRepository,
) *Service {
	return &Service{workspaces: ws, repositories: repos, worktrees: wt, settings: settings}
}

// root returns the configured worktree root with "~" expanded and made
// absolute, falling back to the built-in default when unset or unreadable. It is
// absolute because git resolves relative paths against the repository, not
// against this process.
func (s *Service) root() string {
	value := ""
	if s.settings != nil {
		if v, err := s.settings.Get(domain.SettingWorkspacesRoot); err == nil {
			value = strings.TrimSpace(v)
		}
	}
	if value == "" {
		value = defaultWorktreesRoot
	}
	expanded := domain.ExpandUserPath(value)
	if abs, err := filepath.Abs(expanded); err == nil {
		return abs
	}
	return expanded
}

// Create materializes a git worktree for the repository and persists the
// workspace. branch defaults to the workspace slug when empty; baseRef is the
// ref the branch is cut from, HEAD when empty. On persistence failure the
// worktree is removed so no orphaned directory is left behind.
func (s *Service) Create(repositoryID, name, branch, baseRef string) (*domain.Workspace, error) {
	repo := s.repositories.Get(repositoryID)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	slug := domain.Slug(name)
	if slug == "" {
		return nil, &domain.StructuredError{Code: "INVALID_NAME", Message: "workspace name is required"}
	}
	for _, w := range s.workspaces.ListByRepository(repositoryID) {
		if w.Name == slug {
			return nil, &domain.StructuredError{Code: "WORKSPACE_EXISTS", Message: "workspace " + slug + " already exists for this repository"}
		}
	}
	if branch == "" {
		branch = slug
	}
	// The directory is keyed by name *and* ID: workspace-name uniqueness is
	// only enforced per repository, so two projects each holding a repo named
	// e.g. "api" would otherwise share one directory under the global root.
	repoSlug := domain.Slug(repo.Name)
	if repoSlug == "" {
		repoSlug = repo.ID
	} else {
		repoSlug += "-" + repo.ID
	}
	path := filepath.Join(s.root(), repoSlug, slug)

	if err := s.worktrees.Add(repo.RootDir, path, branch, baseRef); err != nil {
		if errors.Is(err, ports.ErrBranchExists) {
			return nil, &domain.StructuredError{Code: "BRANCH_EXISTS", Message: "branch " + branch + " already exists"}
		}
		return nil, err
	}
	// git does not carry the repository's env files (they are ignored); the
	// harness keeps them and writes them in, so the session can run the app.
	// A worktree missing them would start a session that cannot, so it is
	// removed instead.
	if err := s.writeEnvFiles(repositoryID, path); err != nil {
		_ = s.worktrees.Remove(repo.RootDir, path)
		return nil, &domain.StructuredError{Code: "ENV_FILE_WRITE_FAILED", Message: "writing the repository's env files into the worktree failed: " + err.Error()}
	}
	ws, err := s.workspaces.Create(domain.Workspace{RepositoryID: repositoryID, Name: slug, Branch: branch, Path: path, BaseRef: baseRef})
	if err != nil {
		_ = s.worktrees.Remove(repo.RootDir, path)
		return nil, err
	}
	return ws, nil
}

func (s *Service) writeEnvFiles(repositoryID, worktree string) error {
	if s.envFiles == nil || s.envIO == nil {
		return nil
	}
	files := s.envFiles.List(repositoryID)
	if len(files) == 0 {
		return nil
	}
	return s.envIO.Write(worktree, files)
}

// ListBranches returns the refs a new workspace can be based on.
func (s *Service) ListBranches(repositoryID string) ([]domain.GitBranch, error) {
	repo := s.repositories.Get(repositoryID)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	return s.worktrees.ListBranches(repo.RootDir)
}

// checkout is the repository's own checkout directory.
func (s *Service) checkout(repositoryID string) (string, error) {
	repo := s.repositories.Get(repositoryID)
	if repo == nil {
		return "", &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if repo.RootDir == "" {
		return "", &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	return repo.RootDir, nil
}

// RepositoryLog reads the repository's commit graph.
func (s *Service) RepositoryLog(ctx context.Context, repositoryID string, q ports.HistoryQuery) ([]domain.Commit, error) {
	if s.history == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "repository history is not available"}
	}
	dir, err := s.checkout(repositoryID)
	if err != nil {
		return nil, err
	}
	return s.history.Log(ctx, dir, q)
}

// RepositoryCommit reads one commit of the repository.
func (s *Service) RepositoryCommit(ctx context.Context, repositoryID, hash string) (*domain.CommitDetail, error) {
	if s.history == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "repository history is not available"}
	}
	dir, err := s.checkout(repositoryID)
	if err != nil {
		return nil, err
	}
	return s.history.Show(ctx, dir, hash)
}

// WorkspaceHistory is a workspace's branch beside the one it was cut from:
// the commits of both (so the fork point shows), how far they diverged,
// and what the worktree has not committed. A workspace from before base
// refs were recorded is compared with the repository's checked-out branch.
func (s *Service) WorkspaceHistory(ctx context.Context, workspaceID string, limit int) (*domain.WorkspaceHistory, error) {
	if s.history == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "repository history is not available"}
	}
	ws := s.workspaces.Get(workspaceID)
	if ws == nil {
		return nil, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	root, err := s.checkout(ws.RepositoryID)
	if err != nil {
		return nil, err
	}
	out := &domain.WorkspaceHistory{
		WorkspaceID: ws.ID, RepositoryID: ws.RepositoryID, Branch: ws.Branch, Base: ws.BaseRef,
		Changes: []domain.WorkingChange{},
	}
	if out.Base == "" {
		if st, err := s.history.Status(ctx, root); err == nil {
			out.Base = st.Branch
		}
	}

	st, err := s.history.Status(ctx, ws.Path)
	switch {
	case errs.Code(err) == "WORKSPACE_MISSING":
		out.Gone = true
	case err != nil:
		return nil, err
	default:
		out.Head, out.Changes = st.Head, st.Changes
	}

	refs := []string{ws.Branch}
	if out.Base != "" && out.Base != ws.Branch {
		refs = append(refs, out.Base)
		if out.Ahead, out.Behind, err = s.history.Divergence(ctx, root, out.Base, ws.Branch); err != nil {
			out.Ahead, out.Behind = 0, 0 // a base that no longer resolves: no counts
		}
	}
	// The repository's checkout holds every branch; read the graph there,
	// so it shows even after the worktree is gone.
	if out.Commits, err = s.history.Log(ctx, root, ports.HistoryQuery{Limit: limit, Refs: refs}); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns the workspace by id, or nil if not found.
func (s *Service) Get(id string) *domain.Workspace { return s.workspaces.Get(id) }

// ListByRepository returns all workspaces of the given repository.
func (s *Service) ListByRepository(repositoryID string) []*domain.Workspace {
	return s.workspaces.ListByRepository(repositoryID)
}

// Delete removes the worktree from disk, then deletes the row. The row is kept
// when worktree removal fails so the workspace stays visible and retryable.
// The branch is left in place: it is what a deleted session's committed work
// survives on. Use Discard, never Delete, for spawn rollback.
func (s *Service) Delete(id string) error {
	ws := s.workspaces.Get(id)
	if ws == nil {
		return &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	if repo := s.repositories.Get(ws.RepositoryID); repo != nil && repo.RootDir != "" {
		if err := s.worktrees.Remove(repo.RootDir, ws.Path); err != nil {
			return err
		}
	}
	return s.workspaces.Delete(id)
}

// Discard removes the worktree, force-deletes its branch, then deletes the
// row. It exists only for spawn rollback: when provisioning succeeds but a
// later step in Start fails, the branch is seconds old and cut straight from
// baseRef, so deleting it loses nothing and — unlike Delete — prevents it from
// surviving to poison a retry with a stale BRANCH_EXISTS. Session deletion
// must keep using Delete, which preserves the branch.
func (s *Service) Discard(id string) error {
	ws := s.workspaces.Get(id)
	if ws == nil {
		return &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	if repo := s.repositories.Get(ws.RepositoryID); repo != nil && repo.RootDir != "" {
		if err := s.worktrees.Remove(repo.RootDir, ws.Path); err != nil {
			return err
		}
		if err := s.worktrees.DeleteBranch(repo.RootDir, ws.Branch); err != nil {
			return err
		}
	}
	return s.workspaces.Delete(id)
}
