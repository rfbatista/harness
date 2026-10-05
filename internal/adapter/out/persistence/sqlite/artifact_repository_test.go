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

func TestArtifactRepo_DeleteAndDeleteBySession(t *testing.T) {
	r := newArtifactRepo(t)
	a, _ := r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactPage, Title: "A", Path: "a.html", Revision: 1})
	r.Create(&domain.Artifact{SessionID: "s1", Kind: domain.ArtifactPage, Title: "B", Path: "b.html", Revision: 1})
	r.Create(&domain.Artifact{SessionID: "s2", Kind: domain.ArtifactPage, Title: "C", Path: "c.html", Revision: 1})

	if err := r.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(a.ID); err == nil || err.Error() != "ARTIFACT_NOT_FOUND: artifact not found" {
		t.Fatalf("second delete = %v", err)
	}
	if err := r.DeleteBySession("s1"); err != nil {
		t.Fatal(err)
	}
	if left := r.List(ports.ArtifactFilter{SessionID: "s1"}); len(left) != 0 {
		t.Fatalf("s1 still has %+v", left)
	}
	if left := r.List(ports.ArtifactFilter{SessionID: "s2"}); len(left) != 1 {
		t.Fatalf("s2 lost its artifact: %+v", left)
	}
}
