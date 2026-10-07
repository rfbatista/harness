package sqlite

import (
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func newArtifactRepo(t *testing.T) *ArtifactRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return NewArtifactRepository(db)
}

func TestArtifactRepo_CreateGetFindByTarget(t *testing.T) {
	r := newArtifactRepo(t)
	a, err := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ArtifactPage,
		Title: "Card", Path: "design/card.html", Mime: "text/html", SizeBytes: 12, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Revision != 1 || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Fatalf("create = %+v", a)
	}
	if got := r.Get(a.ID); got == nil || got.Path != "design/card.html" || got.Kind != domain.ArtifactPage {
		t.Fatalf("get = %+v", got)
	}
	if got := r.FindByTarget("s1", "design/card.html", ""); got == nil || got.ID != a.ID {
		t.Fatalf("find by path = %+v", got)
	}
	if got := r.FindByTarget("s2", "design/card.html", ""); got != nil {
		t.Fatalf("another session must not find it: %+v", got)
	}
	if got := r.Get("ghost"); got != nil {
		t.Fatalf("ghost = %+v", got)
	}
}

func TestArtifactRepo_TwoURLArtifactsInOneSession(t *testing.T) {
	r := newArtifactRepo(t)
	for _, u := range []string{"http://localhost:3000", "http://localhost:4000"} {
		if _, err := r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactURL, Title: u, URL: u, Revision: 1}); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
	if got := r.FindByTarget("s1", "", "http://localhost:4000"); got == nil || got.URL != "http://localhost:4000" {
		t.Fatalf("find by url = %+v", got)
	}
}

func TestArtifactRepo_UpdateAndListOrder(t *testing.T) {
	r := newArtifactRepo(t)
	first, _ := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", Kind: domain.ArtifactPage, Title: "A", Path: "a.html", Revision: 1})
	time.Sleep(2 * time.Millisecond)
	second, _ := r.Create(&domain.Artifact{SessionID: "s2", TicketID: "tk1", Kind: domain.ArtifactImage, Title: "B", Path: "b.png", Revision: 1})

	list := r.List(ports.ArtifactFilter{TicketID: "tk1"})
	if len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("newest first expected, got %+v", list)
	}
	time.Sleep(2 * time.Millisecond)
	first.Title, first.Note, first.Revision, first.SizeBytes, first.UpdatedAt = "A2", "tweaked", 2, 99, time.Now()
	if err := r.Update(first); err != nil {
		t.Fatal(err)
	}
	got := r.Get(first.ID)
	if got.Title != "A2" || got.Note != "tweaked" || got.Revision != 2 || got.SizeBytes != 99 {
		t.Fatalf("update lost fields: %+v", got)
	}
	if list = r.List(ports.ArtifactFilter{TicketID: "tk1"}); list[0].ID != first.ID {
		t.Fatalf("updated artifact must list first, got %+v", list)
	}
	if list = r.List(ports.ArtifactFilter{SessionID: "s2"}); len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("session filter = %+v", list)
	}
}

func TestArtifactRepo_DeleteAndDeleteTaskScopedBySession(t *testing.T) {
	r := newArtifactRepo(t)
	a, _ := r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactPage, Title: "A", Path: "a.html", Revision: 1})
	b, _ := r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactPage, Title: "B", Path: "b.html", Revision: 1})
	kept, _ := r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactPage, Title: "K", Path: "k.html", Revision: 1, Scope: domain.ArtifactScopeProject})
	r.Create(&domain.Artifact{SessionID: "s2", Kind: domain.ArtifactPage, Title: "C", Path: "c.html", Revision: 1})

	if err := r.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(a.ID); err == nil || err.Error() != "ARTIFACT_NOT_FOUND: artifact not found" {
		t.Fatalf("second delete = %v", err)
	}
	ids, err := r.DeleteTaskScopedBySession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("deleted ids = %v, want [%s]", ids, b.ID)
	}
	if left := r.List(ports.ArtifactFilter{SessionID: "s1"}); len(left) != 1 || left[0].ID != kept.ID {
		t.Fatalf("s1 should keep only its project artifact, has %+v", left)
	}
	if left := r.List(ports.ArtifactFilter{SessionID: "s2"}); len(left) != 1 {
		t.Fatalf("s2 lost its artifact: %+v", left)
	}
}

func TestArtifactRepo_ScopeDefaultsAndFilters(t *testing.T) {
	r := newArtifactRepo(t)
	task, _ := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ArtifactPage, Title: "T", Path: "t.html", Revision: 1})
	if task.Scope != domain.ArtifactScopeTask || task.Snapshot {
		t.Fatalf("a new artifact is task-scoped with no snapshot: %+v", task)
	}
	// A row written before the column existed has no scope; it reads as task.
	legacy, _ := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ArtifactPage, Title: "L", Path: "l.html", Revision: 1})
	if err := r.db.Exec("UPDATE artifacts SET scope = NULL WHERE id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got := r.Get(legacy.ID); got.Scope != domain.ArtifactScopeTask {
		t.Fatalf("legacy row scope = %q", got.Scope)
	}
	proj, _ := r.Create(&domain.Artifact{SessionID: "s2", TicketID: "tk2", ProjectID: "p1", Kind: domain.ArtifactImage, Title: "P", Path: "p.png", Revision: 1,
		Scope: domain.ArtifactScopeProject, Snapshot: true})
	r.Create(&domain.Artifact{SessionID: "s3", TicketID: "tk3", ProjectID: "p2", Kind: domain.ArtifactImage, Title: "O", Path: "o.png", Revision: 1, Scope: domain.ArtifactScopeProject})

	if got := r.Get(proj.ID); got.Scope != domain.ArtifactScopeProject || !got.Snapshot {
		t.Fatalf("project artifact = %+v", got)
	}
	if got := r.List(ports.ArtifactFilter{ProjectID: "p1"}); len(got) != 3 {
		t.Fatalf("project filter = %d artifacts", len(got))
	}
	if got := r.List(ports.ArtifactFilter{ProjectID: "p1", Scope: domain.ArtifactScopeProject}); len(got) != 1 || got[0].ID != proj.ID {
		t.Fatalf("project scope = %+v", got)
	}
	if got := r.List(ports.ArtifactFilter{TicketID: "tk1", Scope: domain.ArtifactScopeTask}); len(got) != 2 {
		t.Fatalf("task scope must count the legacy row, got %+v", got)
	}
	if got := r.List(ports.ArtifactFilter{TicketID: "tk1", Scope: domain.ArtifactScopeProject}); len(got) != 0 {
		t.Fatalf("tk1 has no project artifacts, got %+v", got)
	}
}

func TestArtifactRepo_SetScope(t *testing.T) {
	r := newArtifactRepo(t)
	a, _ := r.Create(&domain.Artifact{SessionID: "s1", ProjectID: "p1", Kind: domain.ArtifactPage, Title: "A", Path: "a.html", Revision: 3})
	time.Sleep(2 * time.Millisecond)
	got, err := r.SetScope(a.ID, domain.ArtifactScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != domain.ArtifactScopeProject || !got.Snapshot || got.Revision != 3 || got.Title != "A" {
		t.Fatalf("set scope = %+v", got)
	}
	if !got.UpdatedAt.After(a.UpdatedAt) {
		t.Fatalf("updated_at must move: %v -> %v", a.UpdatedAt, got.UpdatedAt)
	}
	// Update rewrites the snapshot flag and never the scope.
	got.Snapshot, got.Revision = false, 4
	if err := r.Update(got); err != nil {
		t.Fatal(err)
	}
	if again := r.Get(a.ID); again.Snapshot || again.Scope != domain.ArtifactScopeProject {
		t.Fatalf("after update = %+v", again)
	}
	if _, err := r.SetScope("ghost", domain.ArtifactScopeTask, false); err == nil || err.Error() != "ARTIFACT_NOT_FOUND: artifact not found" {
		t.Fatalf("ghost = %v", err)
	}
}
