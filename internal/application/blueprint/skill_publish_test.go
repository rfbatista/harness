package blueprint

import (
	"errors"
	"fmt"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/ports"
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

// newPublishTestService returns a service backed by an in-memory database with
// publishing wired to a recording fake and the given publish root. The agent
// repository is real because DeleteSkill unlinks skills from agents.
func newPublishTestService(t *testing.T, root string) (*Service, *fakePublisher) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	settings := sqlite.NewSettingsRepository(db)
	if root != "" {
		if err := settings.Set(domain.SettingSkillsPublishRoot, root); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(
		nil, nil, nil,
		sqlite.NewAgentRepository(db),
		nil,
		sqlite.NewSkillRepository(db),
		nil, nil, nil, nil, "",
	).WithPublishing(settings, pub)
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

func TestUpdateSettings_MergesKeys(t *testing.T) {
	svc, _ := newPublishTestService(t, "/tmp/pub-root")

	if _, err := svc.UpdateSettings(map[string]string{"other.key": "v"}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if all[domain.SettingSkillsPublishRoot] != "/tmp/pub-root" || all["other.key"] != "v" {
		t.Fatalf("settings should merge, got %v", all)
	}
}

func TestUpdateSettings_RejectsRelativeRoot(t *testing.T) {
	svc, _ := newPublishTestService(t, "")

	_, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "relative/dir"})
	if codeOf(err) != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %v", err)
	}
	// A rejected root must not be persisted: nothing should have reached the
	// settings store, so the key stays absent/unset.
	all, err := svc.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := all[domain.SettingSkillsPublishRoot]; ok && v != "" {
		t.Fatalf("rejected root must not be persisted, got %q", v)
	}
}

func TestUpdateSettings_MigratesPublishedSkills(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/old-root")
	skill := mustCreateSkill(t, svc, "demo")
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil

	if _, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/new-root"}); err != nil {
		t.Fatal(err)
	}

	want := []string{"publish:/tmp/new-root/demo force=true", "unpublish:/tmp/old-root/demo"}
	if len(pub.calls) != len(want) {
		t.Fatalf("publisher calls = %v, want %v", pub.calls, want)
	}
	for i, call := range want {
		if pub.calls[i] != call {
			t.Fatalf("call %d = %q, want %q", i, pub.calls[i], call)
		}
	}
	got := svc.GetSkill(skill.ID)
	if got.PublishedPath != "/tmp/new-root/demo" || got.PublishError != "" {
		t.Fatalf("skill after migration: %+v", got)
	}
}

func TestUpdateSettings_MigrationFailureKeepsOldFolder(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/old-root")
	skill := mustCreateSkill(t, svc, "demo")
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil
	pub.failWith = errors.New("read-only filesystem")

	if _, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/new-root"}); err != nil {
		t.Fatalf("the setting still changes: %v", err)
	}
	for _, call := range pub.calls {
		if call == "unpublish:/tmp/old-root/demo" {
			t.Fatal("the old folder must be left in place when the move failed")
		}
	}
	if got := svc.GetSkill(skill.ID); got.PublishError == "" {
		t.Fatal("the failure should be recorded on the skill")
	}
}

// TestUpdateSettings_ClearingRootLeavesFoldersAlone covers migratePublished's
// cleared-root branch: setting the publish root to "" must not touch the
// filesystem at all (no publish, no unpublish/delete of the existing folder)
// and must not silently unpublish the skill. It only records that the skill
// is no longer synced, so the UI can offer a resync once a root is set again.
func TestUpdateSettings_ClearingRootLeavesFoldersAlone(t *testing.T) {
	svc, pub := newPublishTestService(t, "/tmp/old-root")
	skill := mustCreateSkill(t, svc, "demo")
	if _, err := svc.PublishSkill(skill.ID, false); err != nil {
		t.Fatal(err)
	}
	pub.calls = nil

	if _, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: ""}); err != nil {
		t.Fatal(err)
	}

	if len(pub.calls) != 0 {
		t.Fatalf("clearing the root must not touch the filesystem: %v", pub.calls)
	}
	got := svc.GetSkill(skill.ID)
	if got.PublishedSlug != "demo" {
		t.Fatalf("clearing the root must not silently unpublish the skill: %+v", got)
	}
	if got.PublishError == "" {
		t.Fatal("the skill should be recorded as no longer synced")
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
	if err := svc.Settings.Set(domain.SettingSkillsPublishRoot, ""); err != nil {
		t.Fatal(err)
	}
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

// Unlinking a deleted skill from the agents that reference it is the one piece
// of skill deletion that stayed in this application: harnesskit owns the
// delete, and reaches back through skill.Hooks.BeforeDeleteRow.
func TestDeleteSkill_UnlinksAgents(t *testing.T) {
	svc, _ := newPublishTestService(t, "")
	sk := mustCreateSkill(t, svc, "shared")
	other := mustCreateSkill(t, svc, "kept")

	agent, err := svc.CreateAgent("reviewer", "", "", []string{sk.ID, other.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteSkill(sk.ID); err != nil {
		t.Fatalf("DeleteSkill() error = %v", err)
	}

	updated := svc.GetAgent(agent.ID)
	if updated == nil {
		t.Fatal("the agent should survive the skill deletion")
	}
	for _, id := range updated.SkillIDs {
		if id == sk.ID {
			t.Fatalf("the deleted skill is still linked: %v", updated.SkillIDs)
		}
	}
	if len(updated.SkillIDs) != 1 || updated.SkillIDs[0] != other.ID {
		t.Fatalf("SkillIDs = %v, want just the surviving skill", updated.SkillIDs)
	}
}

// A hook failure must cancel the delete rather than leaving a half-applied state.
func TestDeleteSkill_AgentUpdateFailureCancelsTheDelete(t *testing.T) {
	svc, _ := newPublishTestService(t, "")
	sk := mustCreateSkill(t, svc, "shared")
	if _, err := svc.CreateAgent("reviewer", "", "", []string{sk.ID}, nil); err != nil {
		t.Fatal(err)
	}
	svc.Agents = failingAgentRepo{svc.Agents}

	if err := svc.DeleteSkill(sk.ID); err == nil {
		t.Fatal("DeleteSkill should fail when the agent unlink fails")
	}
	if svc.GetSkill(sk.ID) == nil {
		t.Fatal("the skill must survive a failed unlink")
	}
}

// failingAgentRepo fails every Update, leaving reads intact.
type failingAgentRepo struct{ ports.AgentRepository }

func (f failingAgentRepo) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	return nil, &domain.StructuredError{Code: "INTERNAL", Message: "agent store is down"}
}
