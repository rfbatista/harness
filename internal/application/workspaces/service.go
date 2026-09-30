// Package workspaces provides use-cases for repository workspaces: isolated
// git worktrees materialized on disk and tracked in the database. It is kept
// separate from the catalog contexts so the workspace domain stays isolated.
package workspaces

import (
	"errors"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var _ ports.Workspaces = (*Service)(nil)

// defaultWorktreesRoot is where worktrees land when workspaces.root is unset: a
// single folder outside every checkout, so no worktree pollutes a repository.
const defaultWorktreesRoot = "~/.coding-pool/worktrees"

// Service implements workspace use-cases over the outbound ports.
type Service struct {
	workspaces   ports.WorkspaceRepository
	repositories ports.RepositoryRepository
	worktrees    ports.WorktreeManager
	settings     ports.SettingsRepository
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
	ws, err := s.workspaces.Create(repositoryID, slug, branch, path)
	if err != nil {
		_ = s.worktrees.Remove(repo.RootDir, path)
		return nil, err
	}
	return ws, nil
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
