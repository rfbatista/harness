package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func pr(ws string, n int, state domain.PullRequestState, created time.Time) *domain.PullRequest {
	return &domain.PullRequest{
		WorkspaceID: ws, RepositoryID: "r1", ProjectID: "p1", Number: n,
		URL: "https://github.com/acme/widgets/pull/" + string(rune('0'+n)), Title: "T", HeadBranch: "agent/x",
		BaseBranch: "main", State: state, CreatedAt: created,
	}
}

func TestPullRequestRepo_SaveUpsertsOnRepositoryAndNumber(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewPullRequestRepository(db)
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	if got := r.LatestForWorkspace("w1"); got != nil {
		t.Fatalf("empty repo answered %+v", got)
	}
	saved, err := r.Save(pr("w1", 3, domain.PullRequestOpen, t0))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Number != 3 || saved.ProjectID != "p1" || saved.UpdatedAt.IsZero() {
		t.Fatalf("saved = %+v", saved)
	}
	changed := pr("w1", 3, domain.PullRequestMerged, t0)
	changed.Draft = true
	if _, err := r.Save(changed); err != nil {
		t.Fatal(err)
	}
	got := r.LatestForWorkspace("w1")
	if got == nil || got.State != domain.PullRequestMerged || !got.Draft || !got.CreatedAt.Equal(t0) {
		t.Fatalf("after upsert = %+v", got)
	}
	var n int64
	db.Model(&PullRequestModel{}).Count(&n)
	if n != 1 {
		t.Fatalf("rows = %d, want the one upserted", n)
	}
}

func TestPullRequestRepo_LatestIsTheNewestOfTheWorkspace(t *testing.T) {
	db, _ := Open(":memory:")
	r := NewPullRequestRepository(db)
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	_, _ = r.Save(pr("w1", 1, domain.PullRequestClosed, t0))
	_, _ = r.Save(pr("w1", 2, domain.PullRequestOpen, t0.Add(time.Hour)))
	_, _ = r.Save(pr("w2", 5, domain.PullRequestOpen, t0.Add(2*time.Hour)))
	if got := r.LatestForWorkspace("w1"); got == nil || got.Number != 2 {
		t.Fatalf("latest w1 = %+v, want #2", got)
	}
}

func TestPullRequestRepo_DeleteByProject(t *testing.T) {
	db, _ := Open(":memory:")
	r := NewPullRequestRepository(db)
	t0 := time.Now()
	_, _ = r.Save(pr("w1", 1, domain.PullRequestOpen, t0))
	other := pr("w9", 9, domain.PullRequestOpen, t0)
	other.ProjectID = "p2"
	_, _ = r.Save(other)
	if err := r.DeleteByProject("p1"); err != nil {
		t.Fatal(err)
	}
	if r.LatestForWorkspace("w1") != nil {
		t.Fatal("p1's record survived its project")
	}
	if r.LatestForWorkspace("w9") == nil {
		t.Fatal("p2's record went with p1")
	}
}

func TestPullRequestRepo_RecordSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPullRequestRepository(db).Save(pr("w1", 4, domain.PullRequestOpen, time.Now())); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := NewPullRequestRepository(db2).LatestForWorkspace("w1"); got == nil || got.Number != 4 {
		t.Fatalf("after reopen = %+v", got)
	}
}
