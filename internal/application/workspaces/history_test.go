package workspaces_test

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// recordingHistory answers with one commit and records where it was asked
// and which refs it logged. Status answers per directory; a directory
// without one is gone.
type recordingHistory struct {
	dirs   []string
	refs   [][]string
	status map[string]*domain.WorktreeStatus
}

func (h *recordingHistory) Log(_ context.Context, dir string, q ports.HistoryQuery) ([]domain.Commit, error) {
	h.dirs = append(h.dirs, dir)
	h.refs = append(h.refs, q.Refs)
	return []domain.Commit{{Hash: "abc123", Subject: "init"}}, nil
}
func (h *recordingHistory) Status(_ context.Context, dir string) (*domain.WorktreeStatus, error) {
	if st, ok := h.status[dir]; ok {
		return st, nil
	}
	return nil, &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "gone"}
}
func (h *recordingHistory) Divergence(context.Context, string, string, string) (int, int, error) {
	return 2, 1, nil
}
func (h *recordingHistory) Show(_ context.Context, dir, hash string) (*domain.CommitDetail, error) {
	h.dirs = append(h.dirs, dir)
	return &domain.CommitDetail{Commit: domain.Commit{Hash: hash}}, nil
}

func TestHistoryReadsTheRepositorysOwnCheckout(t *testing.T) {
	svc, _, _, repo := newService(t)
	ctx := context.Background()
	if _, err := svc.RepositoryLog(ctx, repo.ID, ports.HistoryQuery{}); errs.Code(err) != "UNAVAILABLE" {
		t.Fatalf("without a history adapter: %v", err)
	}
	h := &recordingHistory{}
	svc.UseHistory(h)
	commits, err := svc.RepositoryLog(ctx, repo.ID, ports.HistoryQuery{})
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %v", commits, err)
	}
	if _, err := svc.RepositoryCommit(ctx, repo.ID, "abc123"); err != nil {
		t.Fatal(err)
	}
	if len(h.dirs) != 2 || h.dirs[0] != repo.RootDir || h.dirs[1] != repo.RootDir {
		t.Fatalf("asked in %v, want the checkout %q", h.dirs, repo.RootDir)
	}
	if _, err := svc.RepositoryLog(ctx, "nope", ports.HistoryQuery{}); errs.Code(err) != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("unknown repository: %v", err)
	}
}

func TestWorkspaceHistoryComparesTheBranchWithItsBase(t *testing.T) {
	svc, _, _, repo := newService(t)
	ctx := context.Background()
	ws, err := svc.Create(repo.ID, "feed", "agent/feed", "develop")
	if err != nil {
		t.Fatal(err)
	}
	h := &recordingHistory{status: map[string]*domain.WorktreeStatus{
		ws.Path:      {Head: "abc123", Branch: "agent/feed", Changes: []domain.WorkingChange{{Path: "feed.go", Status: " M", Added: 3}}},
		repo.RootDir: {Branch: "main"},
	}}
	svc.UseHistory(h)

	got, err := svc.WorkspaceHistory(ctx, ws.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got.Branch != "agent/feed" || got.Base != "develop" || got.Ahead != 2 || got.Behind != 1 || got.Head != "abc123" || len(got.Changes) != 1 || got.Gone {
		t.Fatalf("history = %+v", got)
	}
	if last := h.refs[len(h.refs)-1]; len(last) != 2 || last[0] != "agent/feed" || last[1] != "develop" {
		t.Fatalf("logged refs %v, want the branch and its recorded base", last)
	}
	if h.dirs[len(h.dirs)-1] != repo.RootDir {
		t.Error("the graph is read in the repository's checkout, which outlives the worktree")
	}

	// A worktree made before bases were recorded compares with the checked-out branch.
	old, err := svc.Create(repo.ID, "old", "agent/old", "")
	if err != nil {
		t.Fatal(err)
	}
	h.status[old.Path] = &domain.WorktreeStatus{Head: "def456", Branch: "agent/old"}
	if got, _ := svc.WorkspaceHistory(ctx, old.ID, 50); got.Base != "main" {
		t.Errorf("fallback base = %q, want main", got.Base)
	}

	// A deleted session's worktree is gone: the graph still shows.
	delete(h.status, ws.Path)
	got, err = svc.WorkspaceHistory(ctx, ws.ID, 50)
	if err != nil || !got.Gone || len(got.Commits) != 1 {
		t.Fatalf("gone worktree: %+v %v", got, err)
	}

	if _, err := svc.WorkspaceHistory(ctx, "nope", 50); errs.Code(err) != "WORKSPACE_NOT_FOUND" {
		t.Fatalf("unknown workspace: %v", err)
	}
}
