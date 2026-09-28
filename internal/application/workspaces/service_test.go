package workspaces_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeSettings is an in-memory ports.SettingsRepository.
type fakeSettings map[string]string

func (f fakeSettings) All() (map[string]string, error) { return f, nil }
func (f fakeSettings) Get(key string) (string, error)  { return f[key], nil }
func (f fakeSettings) Set(key, value string) error     { f[key] = value; return nil }

// fakeWorktree records Add/Remove/DeleteBranch calls and can inject errors.
type fakeWorktree struct {
	addErr          error
	removeErr       error
	deleteBranchErr error
	branches        []domain.GitBranch
	added           []string
	removed         []string
	deletedBranches []string
	lastBranch      string
	lastBaseRef     string
}

func (f *fakeWorktree) Add(repoRoot, path, branch, baseRef string) error {
	f.lastBranch, f.lastBaseRef = branch, baseRef
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, path)
	return nil
}

func (f *fakeWorktree) Remove(repoRoot, path string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, path)
	return nil
}

func (f *fakeWorktree) DeleteBranch(repoRoot, branch string) error {
	if f.deleteBranchErr != nil {
		return f.deleteBranchErr
	}
	f.deletedBranches = append(f.deletedBranches, branch)
	return nil
}

func (f *fakeWorktree) ListBranches(repoRoot string) ([]domain.GitBranch, error) {
	return f.branches, nil
}

// fakeWorkspaceRepo is an in-memory ports.WorkspaceRepository with an
// injectable Create error so the rollback path can be exercised.
type fakeWorkspaceRepo struct {
	createErr error
	seq       atomic.Int64
	items     map[string]*domain.Workspace
}

func newFakeWorkspaceRepo() *fakeWorkspaceRepo {
	return &fakeWorkspaceRepo{items: map[string]*domain.Workspace{}}
}

func (f *fakeWorkspaceRepo) Get(id string) *domain.Workspace { return f.items[id] }

func (f *fakeWorkspaceRepo) ListByRepository(repositoryID string) []*domain.Workspace {
	var out []*domain.Workspace
	for _, w := range f.items {
		if w.RepositoryID == repositoryID {
			out = append(out, w)
		}
	}
	return out
}

func (f *fakeWorkspaceRepo) Create(repositoryID, name, branch, path string) (*domain.Workspace, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	id := "ws" + string(rune('0'+f.seq.Add(1)))
	w := &domain.Workspace{ID: id, RepositoryID: repositoryID, Name: name, Branch: branch, Path: path}
	f.items[id] = w
	return w, nil
}

func (f *fakeWorkspaceRepo) Delete(id string) error {
	if _, ok := f.items[id]; !ok {
		return &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	delete(f.items, id)
	return nil
}

// newService wires a service with a real sqlite repository repo, the fake
// workspace repo and the fake worktree manager. Returns the repo's RootDir too.
func newService(t *testing.T) (*workspaces.Service, *fakeWorkspaceRepo, *fakeWorktree, *domain.Repository) {
	t.Helper()
	return newServiceWithSettings(t, fakeSettings{})
}

// newServiceWithSettings is newService with the settings store spelled out, for
// the tests that care where worktrees land.
func newServiceWithSettings(t *testing.T, settings fakeSettings) (*workspaces.Service, *fakeWorkspaceRepo, *fakeWorktree, *domain.Repository) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	rootDir := filepath.Join(t.TempDir(), "checkout", "My API")
	repo, err := repos.Create("p1", "My API", "", "https://example.com/api.git", rootDir)
	if err != nil {
		t.Fatal(err)
	}
	wsRepo := newFakeWorkspaceRepo()
	wt := &fakeWorktree{}
	return workspaces.NewService(wsRepo, repos, wt, settings), wsRepo, wt, repo
}

func structuredCode(t *testing.T, err error) string {
	t.Helper()
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("want StructuredError, got %v", err)
	}
	return se.Code
}

func TestCreate_HappyPath(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)

	ws, err := svc.Create(repo.ID, "Feature X", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "feature-x" {
		t.Fatalf("name should be slugified: %+v", ws)
	}
	if ws.Branch != "feature-x" {
		t.Fatalf("branch should default to the slug: %+v", ws)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	want := filepath.Join(home, ".coding-pool", "worktrees", "my-api-"+repo.ID, "feature-x")
	if ws.Path != want {
		t.Fatalf("path = %q, want %q", ws.Path, want)
	}
	if len(wt.added) != 1 || wt.added[0] != want {
		t.Fatalf("worktree add calls: %v", wt.added)
	}
	if got := wsRepo.Get(ws.ID); got == nil {
		t.Fatal("workspace not persisted")
	}
	if got := svc.ListByRepository(repo.ID); len(got) != 1 {
		t.Fatalf("ListByRepository want 1 got %d", len(got))
	}
	if got := svc.Get(ws.ID); got == nil || got.ID != ws.ID {
		t.Fatalf("Get failed: %+v", got)
	}
}

func TestCreate_ExplicitBranch(t *testing.T) {
	svc, _, _, repo := newService(t)
	ws, err := svc.Create(repo.ID, "review", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Branch != "main" {
		t.Fatalf("explicit branch should be kept: %+v", ws)
	}
}

func TestCreate_RepositoryNotFound(t *testing.T) {
	svc, _, wt, _ := newService(t)
	_, err := svc.Create("missing", "x", "", "")
	if code := structuredCode(t, err); code != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
	if len(wt.added) != 0 {
		t.Fatal("no worktree should be created")
	}
}

func TestCreate_RepositoryWithoutRootDir(t *testing.T) {
	svc, _, _, _ := newService(t)
	// A second repository with an empty root dir, created directly in sqlite.
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repo, err := repos.Create("p1", "no-root", "", "https://example.com/x.git", "")
	if err != nil {
		t.Fatal(err)
	}
	svc = workspaces.NewService(newFakeWorkspaceRepo(), repos, &fakeWorktree{}, fakeSettings{})
	_, err = svc.Create(repo.ID, "x", "", "")
	if code := structuredCode(t, err); code != "INVALID_ROOT" {
		t.Fatalf("code = %s", code)
	}
}

func TestCreate_InvalidName(t *testing.T) {
	svc, _, _, repo := newService(t)
	_, err := svc.Create(repo.ID, "///", "", "")
	if code := structuredCode(t, err); code != "INVALID_NAME" {
		t.Fatalf("code = %s", code)
	}
}

func TestCreate_DuplicateName(t *testing.T) {
	svc, _, wt, repo := newService(t)
	if _, err := svc.Create(repo.ID, "feature-x", "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(repo.ID, "Feature X", "", "") // same slug
	if code := structuredCode(t, err); code != "WORKSPACE_EXISTS" {
		t.Fatalf("code = %s", code)
	}
	if len(wt.added) != 1 {
		t.Fatalf("duplicate must not create a second worktree: %v", wt.added)
	}
}

func TestCreate_WorktreeAddFailurePersistsNothing(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	wt.addErr = errors.New("boom")
	if _, err := svc.Create(repo.ID, "x", "", ""); err == nil {
		t.Fatal("expected error")
	}
	if len(wsRepo.items) != 0 {
		t.Fatalf("nothing should be persisted: %v", wsRepo.items)
	}
}

func TestCreate_PersistFailureRemovesWorktree(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	wsRepo.createErr = errors.New("db down")
	if _, err := svc.Create(repo.ID, "x", "", ""); err == nil {
		t.Fatal("expected error")
	}
	if len(wt.removed) != 1 {
		t.Fatalf("orphaned worktree should be removed: added=%v removed=%v", wt.added, wt.removed)
	}
}

func TestDelete_RemovesWorktreeThenRow(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	ws, err := svc.Create(repo.ID, "gone", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ws.ID); err != nil {
		t.Fatal(err)
	}
	if len(wt.removed) != 1 || wt.removed[0] != ws.Path {
		t.Fatalf("worktree remove calls: %v", wt.removed)
	}
	// Session deletion is the teardown path that must NOT touch the branch:
	// committed work has to survive the session it was written in.
	if len(wt.deletedBranches) != 0 {
		t.Fatalf("Delete must not delete the branch, got: %v", wt.deletedBranches)
	}
	if wsRepo.Get(ws.ID) != nil {
		t.Fatal("row should be deleted")
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc, _, _, _ := newService(t)
	err := svc.Delete("missing")
	if code := structuredCode(t, err); code != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
}

func TestDelete_WorktreeRemoveFailureKeepsRow(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	ws, err := svc.Create(repo.ID, "stuck", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wt.removeErr = errors.New("locked")
	if err := svc.Delete(ws.ID); err == nil {
		t.Fatal("expected error")
	}
	if wsRepo.Get(ws.ID) == nil {
		t.Fatal("row must survive a failed worktree removal")
	}
}

// TestDiscard_RemovesWorktreeBranchThenRow is spawn rollback's contract: a
// just-provisioned workspace that Start could not finish using must leave
// nothing behind — worktree, branch and row all go — so a retry never trips
// over a stale BRANCH_EXISTS.
func TestDiscard_RemovesWorktreeBranchThenRow(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	ws, err := svc.Create(repo.ID, "doomed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Discard(ws.ID); err != nil {
		t.Fatal(err)
	}
	if len(wt.removed) != 1 || wt.removed[0] != ws.Path {
		t.Fatalf("worktree remove calls: %v", wt.removed)
	}
	if len(wt.deletedBranches) != 1 || wt.deletedBranches[0] != ws.Branch {
		t.Fatalf("branch delete calls: %v, want [%s]", wt.deletedBranches, ws.Branch)
	}
	if wsRepo.Get(ws.ID) != nil {
		t.Fatal("row should be deleted")
	}
}

func TestDiscard_NotFound(t *testing.T) {
	svc, _, _, _ := newService(t)
	err := svc.Discard("missing")
	if code := structuredCode(t, err); code != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
}

func TestDiscard_WorktreeRemoveFailureKeepsRowAndBranch(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	ws, err := svc.Create(repo.ID, "stuck", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wt.removeErr = errors.New("locked")
	if err := svc.Discard(ws.ID); err == nil {
		t.Fatal("expected error")
	}
	if wsRepo.Get(ws.ID) == nil {
		t.Fatal("row must survive a failed worktree removal")
	}
	if len(wt.deletedBranches) != 0 {
		t.Fatalf("branch must not be deleted when worktree removal fails: %v", wt.deletedBranches)
	}
}

func TestDiscard_BranchDeleteFailureKeepsRow(t *testing.T) {
	svc, wsRepo, wt, repo := newService(t)
	ws, err := svc.Create(repo.ID, "stuck-branch", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wt.deleteBranchErr = errors.New("branch checked out elsewhere")
	if err := svc.Discard(ws.ID); err == nil {
		t.Fatal("expected error")
	}
	if wsRepo.Get(ws.ID) == nil {
		t.Fatal("row must survive a failed branch deletion")
	}
}

func TestService_CreateUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	svc, _, _, repo := newServiceWithSettings(t, fakeSettings{domain.SettingWorkspacesRoot: root})

	ws, err := svc.Create(repo.ID, "Feature X", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "my-api-"+repo.ID, "feature-x")
	if ws.Path != want {
		t.Fatalf("path = %q, want %q", ws.Path, want)
	}
}

func TestService_CreateFallsBackToDefaultRoot(t *testing.T) {
	svc, _, _, repo := newService(t)

	ws, err := svc.Create(repo.ID, "Feature X", "", "")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	want := filepath.Join(home, ".coding-pool", "worktrees", "my-api-"+repo.ID, "feature-x")
	if ws.Path != want {
		t.Fatalf("path = %q, want %q", ws.Path, want)
	}
}

// TestService_CreateKeepsRepositoriesWithTheSameNameApart proves the
// directory is keyed by repository ID as well as name: two repositories that
// both slugify to "api" must not land in the same worktree directory, or a
// workspace/branch collision between unrelated projects becomes a raw git 500.
func TestService_CreateKeepsRepositoriesWithTheSameNameApart(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repos := sqlite.NewRepositoryRepository(db)
	repoA, err := repos.Create("p1", "api", "", "https://example.com/a.git", filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatal(err)
	}
	repoB, err := repos.Create("p2", "api", "", "https://example.com/b.git", filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatal(err)
	}
	svc := workspaces.NewService(newFakeWorkspaceRepo(), repos, &fakeWorktree{}, fakeSettings{})

	wsA, err := svc.Create(repoA.ID, "Feature X", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wsB, err := svc.Create(repoB.ID, "Feature X", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if wsA.Path == wsB.Path {
		t.Fatalf("same-named repositories collided on one worktree directory: %q", wsA.Path)
	}
	if !strings.Contains(wsA.Path, repoA.ID) || !strings.Contains(wsB.Path, repoB.ID) {
		t.Fatalf("paths should be keyed by repository ID: a=%q b=%q", wsA.Path, wsB.Path)
	}
}

func TestService_CreatePassesBaseRefToGit(t *testing.T) {
	svc, _, wt, repo := newService(t)

	if _, err := svc.Create(repo.ID, "Feature X", "feature/x", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if wt.lastBranch != "feature/x" || wt.lastBaseRef != "origin/main" {
		t.Fatalf("git got branch=%q base=%q", wt.lastBranch, wt.lastBaseRef)
	}
}

func TestService_CreateMapsExistingBranchToStructuredError(t *testing.T) {
	svc, _, wt, repo := newService(t)
	wt.addErr = fmt.Errorf("%w: feature-x", ports.ErrBranchExists)

	_, err := svc.Create(repo.ID, "Feature X", "", "")
	if code := structuredCode(t, err); code != "BRANCH_EXISTS" {
		t.Fatalf("Create = %v (code %q), want BRANCH_EXISTS", err, code)
	}
}

func TestService_ListBranches(t *testing.T) {
	svc, _, wt, repo := newService(t)
	wt.branches = []domain.GitBranch{{Name: "main", IsHead: true}}

	got, err := svc.ListBranches(repo.ID)
	if err != nil || len(got) != 1 || got[0].Name != "main" {
		t.Fatalf("ListBranches = %v, %v", got, err)
	}

	_, err = svc.ListBranches("nope")
	if code := structuredCode(t, err); code != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("ListBranches on a missing repo = %v (code %q), want REPOSITORY_NOT_FOUND", err, code)
	}
}
