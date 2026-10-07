package sqlite

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// projectAsset stores a project artifact produced by tk1 and the tickets
// tk1, tk2, tk3 of project p1.
func projectAsset(t *testing.T, r *ArtifactRepository) *domain.Artifact {
	t.Helper()
	for _, id := range []string{"tk1", "tk2", "tk3"} {
		if err := r.db.Create(&TicketModel{ID: id, ProjectID: "p1", Title: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	a, err := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ArtifactPage,
		Title: "Logo", Path: "logo.html", Revision: 1, Scope: domain.ArtifactScopeProject, Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ids(a *domain.Artifact) string { return strings.Join(a.AttachedTicketIDs, ",") }

func listIDs(list []*domain.Artifact) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Title)
	}
	return strings.Join(out, ",")
}

func TestArtifactRepo_AttachDetachAreIdempotent(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	if changed, err := r.Attach(a.ID, "tk2"); err != nil || !changed {
		t.Fatalf("attach = %v %v", changed, err)
	}
	if changed, err := r.Attach(a.ID, "tk2"); err != nil || changed {
		t.Fatalf("attach again = %v %v, want unchanged", changed, err)
	}
	if changed, err := r.Detach(a.ID, "tk2"); err != nil || !changed {
		t.Fatalf("detach = %v %v", changed, err)
	}
	if changed, err := r.Detach(a.ID, "tk2"); err != nil || changed {
		t.Fatalf("detach again = %v %v, want unchanged", changed, err)
	}
}

// Get, FindByTarget and List carry the attached tasks, oldest link first,
// and an attach leaves updated_at alone.
func TestArtifactRepo_AttachedTicketIDsAreFilledInLinkOrder(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	if got := r.Get(a.ID); got.AttachedTicketIDs == nil || len(got.AttachedTicketIDs) != 0 {
		t.Fatalf("no links: %#v, want empty, not nil", got.AttachedTicketIDs)
	}
	r.Attach(a.ID, "tk3")
	time.Sleep(2 * time.Millisecond)
	r.Attach(a.ID, "tk2")
	if got := r.Get(a.ID); ids(got) != "tk3,tk2" || !got.UpdatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("get = %s, updated %v (was %v)", ids(got), got.UpdatedAt, a.UpdatedAt)
	}
	if got := r.FindByTarget("s1", "logo.html", ""); ids(got) != "tk3,tk2" {
		t.Fatalf("find = %s", ids(got))
	}
	if got := r.List(ports.ArtifactFilter{ProjectID: "p1"}); len(got) != 1 || ids(got[0]) != "tk3,tk2" {
		t.Fatalf("list = %d rows", len(got))
	}
}

// Listing by task gives what it produced and what is attached to it, newest
// first, still narrowed by scope; listing by session or project adds nothing.
func TestArtifactRepo_ListByTicketIncludesAttached(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	time.Sleep(2 * time.Millisecond)
	own, _ := r.Create(&domain.Artifact{SessionID: "s2", TicketID: "tk2", ProjectID: "p1", Kind: domain.ArtifactImage, Title: "Own", Path: "own.png", Revision: 1})
	r.Attach(a.ID, "tk2")
	if got := listIDs(r.List(ports.ArtifactFilter{TicketID: "tk2"})); got != "Own,Logo" {
		t.Fatalf("tk2 = %s, want Own,Logo", got)
	}
	if got := listIDs(r.List(ports.ArtifactFilter{TicketID: "tk2", Scope: domain.ArtifactScopeProject})); got != "Logo" {
		t.Fatalf("tk2 project scope = %s", got)
	}
	if got := listIDs(r.List(ports.ArtifactFilter{TicketID: "tk2", Scope: domain.ArtifactScopeTask})); got != "Own" {
		t.Fatalf("tk2 task scope = %s", got)
	}
	if got := listIDs(r.List(ports.ArtifactFilter{TicketID: "tk3"})); got != "" {
		t.Fatalf("tk3 = %s, want nothing", got)
	}
	if got := listIDs(r.List(ports.ArtifactFilter{SessionID: "s2"})); got != "Own" {
		t.Fatalf("session s2 = %s", got)
	}
	_ = own
}

func TestArtifactRepo_MovingToTaskOrDeletingDropsLinks(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	r.Attach(a.ID, "tk2")
	moved, err := r.SetScope(a.ID, domain.ArtifactScopeTask, true)
	if err != nil || ids(moved) != "" {
		t.Fatalf("move to task = %s %v", ids(moved), err)
	}
	if got := listIDs(r.List(ports.ArtifactFilter{TicketID: "tk2"})); got != "" {
		t.Fatalf("tk2 after move = %s", got)
	}
	back, _ := r.SetScope(a.ID, domain.ArtifactScopeProject, true)
	if ids(back) != "" {
		t.Fatalf("moving back attaches nothing: %s", ids(back))
	}
	r.Attach(a.ID, "tk3")
	if err := r.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	r.db.Model(&ArtifactTicketModel{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d links survive the delete", n)
	}
}

func TestArtifactRepo_DetachTicket(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	b, _ := r.Create(&domain.Artifact{SessionID: "s1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ArtifactImage,
		Title: "Palette", Path: "palette.png", Revision: 1, Scope: domain.ArtifactScopeProject})
	r.Attach(a.ID, "tk2")
	r.Attach(b.ID, "tk2")
	r.Attach(a.ID, "tk3")
	got, err := r.DetachTicket("tk2")
	if err != nil || len(got) != 2 {
		t.Fatalf("detach ticket = %v %v", got, err)
	}
	if ids(r.Get(a.ID)) != "tk3" || ids(r.Get(b.ID)) != "" {
		t.Fatalf("after = %s / %s", ids(r.Get(a.ID)), ids(r.Get(b.ID)))
	}
	if got, err := r.DetachTicket("tk2"); err != nil || len(got) != 0 {
		t.Fatalf("again = %v %v", got, err)
	}
}

// A link whose ticket row is gone never shows.
func TestArtifactRepo_OrphanLinkIsHidden(t *testing.T) {
	r := newArtifactRepo(t)
	a := projectAsset(t, r)
	r.Attach(a.ID, "tk2")
	r.db.Delete(&TicketModel{}, "id = ?", "tk2")
	if got := ids(r.Get(a.ID)); got != "" {
		t.Fatalf("orphan link shows: %s", got)
	}
}

// An existing database gains the link table on open.
func TestArtifactRepo_MigratesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&ArtifactTicketModel{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&ArtifactTicketModel{}) {
		t.Fatal("artifact_tickets missing after reopening")
	}
}
