package artifacts

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var ctx = context.Background()

func (f *fixture) view(t *testing.T, id, rel string) string {
	t.Helper()
	af, err := f.svc.OpenArtifactFile(ctx, id, rel)
	if err != nil {
		t.Fatalf("open %s %q: %v", id, rel, err)
	}
	defer af.Content.Close()
	b, _ := io.ReadAll(af.Content)
	return string(b)
}

func (f *fixture) promote(t *testing.T, id string) *domain.Artifact {
	t.Helper()
	a, err := f.svc.SetArtifactScope(ctx, id, domain.ArtifactScopeProject)
	if err != nil {
		t.Fatalf("promote %s: %v", id, err)
	}
	return a
}

func revisionDirs(t *testing.T, store, id string) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(store, id))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSetArtifactScope_ProjectServesTheSnapshot(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "<p>v1</p>")
	writeFile(t, f.root, "design/style.css", "p{}")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card", Note: "first"})
	time.Sleep(2 * time.Millisecond)

	got := f.promote(t, a.ID)
	if got.Scope != domain.ArtifactScopeProject || !got.Snapshot || got.Revision != 1 || got.ID != a.ID || !got.UpdatedAt.After(a.UpdatedAt) {
		t.Fatalf("promoted = %+v", got)
	}
	if n := len(f.ann.events); n != 2 {
		t.Fatalf("want the publish and the move announced, got %d events", n)
	}
	ev := f.ann.events[1]
	if ev.Type != "artifact" || ev.SessionID != "s1" || ev.Text != "" || ev.Artifact == nil || ev.Artifact.Scope != domain.ArtifactScopeProject {
		t.Fatalf("move event = %+v", ev)
	}

	// The worktree changes; the project's copy does not.
	writeFile(t, f.root, "design/card.html", "<p>edited, not published</p>")
	if body := f.view(t, a.ID, ""); body != "<p>v1</p>" {
		t.Fatalf("view = %q, want the snapshot", body)
	}
	// The same scope again changes nothing and says nothing.
	again := f.promote(t, a.ID)
	if !again.UpdatedAt.Equal(got.UpdatedAt) || len(f.ann.events) != 2 {
		t.Fatalf("idempotent move changed something: %+v, %d events", again, len(f.ann.events))
	}
}

func TestSetArtifactScope_OutlivesTheSession(t *testing.T) {
	f := newFixture(t)
	bus := &fakeBus{}
	f.svc.Subscribe(bus)
	writeFile(t, f.root, "design/card.html", "<p>card</p>")
	writeFile(t, f.root, "design/style.css", "p{}")
	writeFile(t, f.root, "notes/scratch.html", "scratch")
	kept := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	dropped := f.publish(t, ports.PublishArtifactRequest{Path: "notes/scratch.html", Title: "Scratch"})
	f.promote(t, kept.ID)
	// A task artifact that once was project still has a copy; it goes too.
	f.promote(t, dropped.ID)
	if _, err := f.svc.SetArtifactScope(ctx, dropped.ID, domain.ArtifactScopeTask); err != nil {
		t.Fatal(err)
	}

	bus.fire(t, domain.SessionDeleted{SessionID: "s1"})
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if f.repo.Get(dropped.ID) != nil {
		t.Fatal("the task artifact must go with its session")
	}
	if _, err := os.Stat(filepath.Join(f.svc.StoreDir, dropped.ID)); !os.IsNotExist(err) {
		t.Fatalf("the task artifact's copy must go too: %v", err)
	}
	if a, err := f.svc.GetArtifact(ctx, kept.ID); err != nil || a.Scope != domain.ArtifactScopeProject {
		t.Fatalf("project artifact = %+v, %v", a, err)
	}
	if body := f.view(t, kept.ID, ""); body != "<p>card</p>" {
		t.Fatalf("view = %q", body)
	}
	if body := f.view(t, kept.ID, "style.css"); body != "p{}" {
		t.Fatalf("sibling = %q", body)
	}
	list, err := f.svc.ListProjectArtifacts(ctx, "p1")
	if err != nil || len(list) != 1 || list[0].ID != kept.ID {
		t.Fatalf("project list = %+v, %v", list, err)
	}
	// Back to task once the session is gone: allowed, still served from the copy.
	back, err := f.svc.SetArtifactScope(ctx, kept.ID, domain.ArtifactScopeTask)
	if err != nil || back.Scope != domain.ArtifactScopeTask || !back.Snapshot {
		t.Fatalf("back to task = %+v, %v", back, err)
	}
	if body := f.view(t, kept.ID, ""); body != "<p>card</p>" {
		t.Fatalf("view after moving back = %q", body)
	}
	// And promoting again needs no worktree: the copy is already there.
	if again := f.promote(t, kept.ID); again.Scope != domain.ArtifactScopeProject {
		t.Fatalf("re-promote = %+v", again)
	}
}

func TestSetArtifactScope_RepublishRefreshesTheSnapshot(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "v1")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	f.promote(t, a.ID)
	writeFile(t, f.root, "design/card.html", "v2")
	writeFile(t, f.root, "design/new.css", "new{}")
	b := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card", Note: "v2"})

	if b.ID != a.ID || b.Revision != 2 || b.Scope != domain.ArtifactScopeProject || !b.Snapshot {
		t.Fatalf("republished = %+v", b)
	}
	if body := f.view(t, a.ID, ""); body != "v2" {
		t.Fatalf("view = %q", body)
	}
	if body := f.view(t, a.ID, "new.css"); body != "new{}" {
		t.Fatalf("new sibling = %q", body)
	}
	if dirs := revisionDirs(t, f.svc.StoreDir, a.ID); len(dirs) != 1 || dirs[0] != "r2" {
		t.Fatalf("revision dirs = %v, want [r2]", dirs)
	}

	// Over the cap: the publish is refused and the record keeps its revision.
	f.svc.MaxSizeBytes = 10
	writeFile(t, f.root, "design/heavy.bin", "0123456789")
	_, err := f.svc.Publish(ctx, ports.PublishArtifactRequest{SessionID: "s1", Path: "design/card.html", Title: "Card"})
	if errs.Code(err) != "ARTIFACT_TOO_LARGE" {
		t.Fatalf("code %q (%v)", errs.Code(err), err)
	}
	if got := f.repo.Get(a.ID); got.Revision != 2 {
		t.Fatalf("refused publish moved the revision: %+v", got)
	}
	if dirs := revisionDirs(t, f.svc.StoreDir, a.ID); len(dirs) != 1 || dirs[0] != "r2" {
		t.Fatalf("revision dirs after refusal = %v", dirs)
	}
}

func TestSetArtifactScope_Refusals(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "0123456789")
	writeFile(t, f.root, "design/more.bin", "0123456789")
	page := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	link := f.publish(t, ports.PublishArtifactRequest{URL: "http://localhost:3000", Title: "Dev"})

	check := func(name, id string, scope domain.ArtifactScope, code string) {
		t.Helper()
		if _, err := f.svc.SetArtifactScope(ctx, id, scope); errs.Code(err) != code {
			t.Errorf("%s: code %q (%v), want %s", name, errs.Code(err), err, code)
		}
	}
	check("bad scope", page.ID, "global", "INVALID_INPUT")
	check("empty scope", page.ID, "", "INVALID_INPUT")
	check("unknown", "ghost", domain.ArtifactScopeProject, "ARTIFACT_NOT_FOUND")
	check("url", link.ID, domain.ArtifactScopeProject, "ARTIFACT_NOT_PROMOTABLE")

	f.svc.MaxSizeBytes = 15
	check("over the cap", page.ID, domain.ArtifactScopeProject, "ARTIFACT_NOT_PROMOTABLE")
	f.svc.MaxSizeBytes = 0

	store := f.svc.StoreDir
	f.svc.StoreDir = ""
	check("no store", page.ID, domain.ArtifactScopeProject, "ARTIFACT_NOT_PROMOTABLE")
	f.svc.StoreDir = store

	if err := os.Remove(filepath.Join(f.root, "design", "card.html")); err != nil {
		t.Fatal(err)
	}
	check("file gone", page.ID, domain.ArtifactScopeProject, "ARTIFACT_NOT_PROMOTABLE")

	if got := f.repo.Get(page.ID); got.Scope != domain.ArtifactScopeTask || got.Snapshot {
		t.Fatalf("refusals must change nothing: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(store, page.ID)); !os.IsNotExist(err) {
		t.Fatalf("refusals must leave no copy: %v", err)
	}
	// Moving a task artifact to task is fine: nothing to do.
	if a, err := f.svc.SetArtifactScope(ctx, link.ID, domain.ArtifactScopeTask); err != nil || a.Scope != domain.ArtifactScopeTask {
		t.Fatalf("url to task = %+v, %v", a, err)
	}
}

func TestUnpublishAndDelete_ProjectArtifacts(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "a/card.html", "a")
	writeFile(t, f.root, "b/card.html", "b")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "a/card.html", Title: "A"})
	b := f.publish(t, ports.PublishArtifactRequest{Path: "b/card.html", Title: "B"})
	f.promote(t, a.ID)
	f.promote(t, b.ID)

	if err := f.svc.Unpublish(ctx, "s2", a.ID); errs.Code(err) != "ARTIFACT_NOT_YOURS" {
		t.Fatalf("another session: %q (%v)", errs.Code(err), err)
	}
	if err := f.svc.Unpublish(ctx, "s1", a.ID); errs.Code(err) != "ARTIFACT_IN_PROJECT" {
		t.Fatalf("project artifact: %q (%v)", errs.Code(err), err)
	}
	if _, err := f.svc.SetArtifactScope(ctx, a.ID, domain.ArtifactScopeTask); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unpublish(ctx, "s1", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.svc.StoreDir, a.ID)); !os.IsNotExist(err) {
		t.Fatalf("unpublish must drop the copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "a", "card.html")); err != nil {
		t.Fatal("unpublish must never touch the worktree:", err)
	}
	// A person deletes a project artifact directly.
	if err := f.svc.DeleteArtifact(ctx, b.ID); err != nil || f.repo.Get(b.ID) != nil {
		t.Fatalf("delete = %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.svc.StoreDir, b.ID)); !os.IsNotExist(err) {
		t.Fatalf("delete must drop the copy: %v", err)
	}
}

func TestListArtifacts_ProjectAndScopeFilters(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "a/card.html", "a")
	writeFile(t, f.root, "b/card.html", "b")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "a/card.html", Title: "A"})
	f.publish(t, ports.PublishArtifactRequest{Path: "b/card.html", Title: "B"})
	f.promote(t, a.ID)

	got, err := f.svc.ListArtifacts(ctx, ports.ArtifactFilter{ProjectID: "p1", Scope: domain.ArtifactScopeProject})
	if err != nil || len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("project scope = %+v, %v", got, err)
	}
	if got, _ := f.svc.ListArtifacts(ctx, ports.ArtifactFilter{TicketID: "tk1", Scope: domain.ArtifactScopeTask}); len(got) != 1 {
		t.Fatalf("task scope = %+v", got)
	}
	if got, _ := f.svc.ListTaskArtifacts(ctx, "tk1"); len(got) != 2 {
		t.Fatalf("the task still lists both scopes: %+v", got)
	}
	if _, err := f.svc.ListArtifacts(ctx, ports.ArtifactFilter{ProjectID: "p1", Scope: "global"}); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("bad scope: %q (%v)", errs.Code(err), err)
	}
	if _, err := f.svc.ListArtifacts(ctx, ports.ArtifactFilter{Scope: domain.ArtifactScopeProject}); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("scope alone: %q (%v)", errs.Code(err), err)
	}
	if _, err := f.svc.ListProjectArtifacts(ctx, ""); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("no project: %q (%v)", errs.Code(err), err)
	}
}

func TestOpenArtifactFile_SnapshotConfinement(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "card")
	writeFile(t, f.root, "design/style.css", "h1{}")
	writeFile(t, f.root, "shared/tokens.css", ":root{}")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	f.promote(t, a.ID)
	writeFile(t, f.svc.StoreDir, "secret.txt", "s")

	if af, err := f.svc.OpenArtifactFile(ctx, a.ID, "style.css"); err != nil || af.Mime != "text/css" || af.Revision != 1 {
		t.Fatalf("sibling = %+v, %v", af, err)
	} else {
		af.Content.Close()
	}
	for _, rel := range []string{"../shared/tokens.css", "../../secret.txt", "../../../../etc/passwd", "nope.css", "."} {
		if _, err := f.svc.OpenArtifactFile(ctx, a.ID, rel); errs.Code(err) != "ARTIFACT_NOT_FOUND" {
			t.Errorf("%q: code %q (%v)", rel, errs.Code(err), err)
		}
	}
}
