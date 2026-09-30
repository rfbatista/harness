package capabilities

import (
	"errors"
	"fmt"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

// fakePublisher records calls and can be made to fail.
type fakePublisher struct {
	calls    []string
	failWith error
}

func (f *fakePublisher) Publish(root, slug string, files []domain.SkillFile, force bool) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("publish:%s/%s force=%v", root, slug, force))
	return root + "/" + slug, f.failWith
}

func (f *fakePublisher) Unpublish(root, slug string) error {
	f.calls = append(f.calls, "unpublish:"+root+"/"+slug)
	return f.failWith
}

func (f *fakePublisher) PutFile(root, slug string, file domain.SkillFile) error {
	f.calls = append(f.calls, "put:"+slug+":"+file.Path)
	return f.failWith
}

func (f *fakePublisher) RenamePath(root, slug, oldPath, newPath string) error {
	f.calls = append(f.calls, "rename:"+slug+":"+oldPath+"->"+newPath)
	return f.failWith
}

func (f *fakePublisher) DeletePath(root, slug, path string) error {
	f.calls = append(f.calls, "delete:"+slug+":"+path)
	return f.failWith
}

func (f *fakePublisher) MoveSlug(root, oldSlug, newSlug string) error {
	f.calls = append(f.calls, "moveslug:"+oldSlug+"->"+newSlug)
	return f.failWith
}

// mapSettings stands in for the settings context.
type mapSettings map[string]string

func (m mapSettings) Setting(key string) string { return m[key] }

// newPublishTestService returns the context over an in-memory database with
// publishing wired to a recording fake and the given publish root.
func newPublishTestService(t *testing.T, root string) (*Service, *fakePublisher) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewService(Deps{
		Skills:    sqlite.NewSkillRepository(db),
		Settings:  mapSettings{domain.SettingSkillsPublishRoot: root},
		Publisher: pub,
	})
	return svc, pub
}

func mustCreateSkill(t *testing.T, svc *Service, name string) *domain.Skill {
	t.Helper()
	skill, err := svc.CreateSkill(domain.SkillInput{
		Name:  name,
		Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# " + name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return skill
}

func codeOf(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestPublishSkill_SetsStateAndDerivesPath(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/pub-root")
	skill := mustCreateSkill(t, svc, "Code Review")

	published, err := svc.PublishSkill(skill.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !published.IsPublished() || published.PublishedSlug != "code-review" {
		t.Fatalf("publish state wrong: %+v", published)
	}
	if published.PublishedPath != "/tmp/pub-root/code-review" {
		t.Fatalf("PublishedPath = %q", published.PublishedPath)
	}
	if published.PublishedAt == nil {
		t.Fatal("PublishedAt should be set")
	}
	if len(pub.calls) != 1 || pub.calls[0] != "publish:/tmp/pub-root/code-review force=false" {
		t.Fatalf("publisher calls = %v", pub.calls)
	}

	// Re-publishing is an idempotent resync over our own folder: force must be
	// implied even though the caller passes force=false, because the publisher
	// would otherwise reject a target directory that already exists.
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatalf("resync: %v", err)
	}
	if len(pub.calls) != 2 || pub.calls[1] != "publish:/tmp/pub-root/code-review force=true" {
		t.Fatalf("resync should pass force=true implicitly: %v", pub.calls)
	}
}

func TestDeleteSkill_RemovesPublishedFolderFirst(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/pub-root")
	skill := mustCreateSkill(t, svc, "demo")
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil

	// A failure to remove the folder aborts the delete: once the row is gone
	// there is nowhere left to record the problem.
	pub.failWith = errors.New("permission denied")
	if err := svc.DeleteSkill(skill.ID); err == nil {
		t.Fatal("expected the delete to fail")
	}
	if svc.GetSkill(skill.ID) == nil {
		t.Fatal("the skill must survive a failed unpublish")
	}

	pub.failWith = nil
	pub.calls = nil
	if err := svc.DeleteSkill(skill.ID); err != nil {
		t.Fatal(err)
	}
	if svc.GetSkill(skill.ID) != nil {
		t.Fatal("the skill should be gone")
	}
	if len(pub.calls) != 1 || pub.calls[0] != "unpublish:/tmp/pub-root/demo" {
		t.Fatalf("expected exactly one unpublish call, got %v", pub.calls)
	}
}

// TestDeleteSkill_RequiresRootWhenPublished covers the same guard PublishSkill
// and UnpublishSkill already have: if the publish root becomes unset later
// (e.g. the setting was cleared) while a skill is still published, deleting
// it must fail closed with PUBLISH_ROOT_NOT_SET rather than call the
// publisher with an empty root — the fake doesn't replicate the real
// adapter's own empty-root rejection, so this guard needs its own test at the
// call site.
func TestDeleteSkill_RequiresRootWhenPublished(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/pub-root")
	skill := mustCreateSkill(t, svc, "demo")
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatal(err)
	}
	svc.settings.(mapSettings)[domain.SettingSkillsPublishRoot] = ""
	pub.calls = nil

	if err := svc.DeleteSkill(skill.ID); codeOf(err) != "PUBLISH_ROOT_NOT_SET" {
		t.Fatalf("expected PUBLISH_ROOT_NOT_SET, got %v", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("nothing should reach the publisher: %v", pub.calls)
	}
	if svc.GetSkill(skill.ID) == nil {
		t.Fatal("the skill must survive when the root guard rejects the delete")
	}
}
