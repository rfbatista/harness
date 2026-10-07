package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// These tests cover what no single context can: the reactions that travel
// between contexts as events, and the read ports they query each other with.

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
func (f *fakePublisher) PutFile(root, slug string, file domain.SkillFile) error { return f.failWith }
func (f *fakePublisher) RenamePath(root, slug, oldPath, newPath string) error {
	return f.failWith
}
func (f *fakePublisher) DeletePath(root, slug, path string) error     { return f.failWith }
func (f *fakePublisher) MoveSlug(root, oldSlug, newSlug string) error { return f.failWith }

type fixture struct {
	Catalog
	deps Deps
	pub  *fakePublisher
}

func newFixture(t *testing.T, tweak ...func(*Deps)) fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	d := SQLiteDeps(db)
	d.Publisher = pub
	for _, f := range tweak {
		f(&d)
	}
	return fixture{Catalog: New(d), deps: d, pub: pub}
}

func must[T any](t *testing.T) func(v T, err error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func (f fixture) skill(t *testing.T, name string) *domain.Skill {
	t.Helper()
	return must[*domain.Skill](t)(f.Capabilities.CreateSkill(domain.SkillInput{
		Name:  name,
		Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# " + name}},
	}))
}

func TestDeleteProject_RemovesItsRepositoriesZonesAndBoundedContexts(t *testing.T) {
	f := newFixture(t)
	p := must[*domain.Project](t)(f.Projects.CreateProject(context.Background(), "proj", t.TempDir()))
	keep := must[*domain.Project](t)(f.Projects.CreateProject(context.Background(), "other", t.TempDir()))
	must[*domain.Repository](t)(f.Projects.CreateRepository(context.Background(), p.ID, "api", "", "https://x/api.git", "/tmp/api"))
	zone := must[*domain.Zone](t)(f.Architecture.CreateZone(p.ID, "api", "", "", nil, nil))
	bc := must[*domain.BoundedContext](t)(f.Architecture.CreateBoundedContext(p.ID, "Billing", "", nil))
	kept := must[*domain.Zone](t)(f.Architecture.CreateZone(keep.ID, "web", "", "", nil, nil))

	if err := f.Projects.DeleteProject(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if repos, _ := f.Projects.ListRepositories(context.Background(), p.ID); len(repos) != 0 {
		t.Error("repositories survived their project")
	}
	if f.Architecture.GetZone(zone.ID) != nil || f.Architecture.GetBoundedContext(bc.ID) != nil {
		t.Error("zones or bounded contexts survived their project")
	}
	if f.Architecture.GetZone(kept.ID) == nil {
		t.Error("another project's zone was deleted")
	}
}

func TestDeletePrompt_UnlinksAgentsAndZoneRules(t *testing.T) {
	f := newFixture(t)
	p := must[*domain.Project](t)(f.Projects.CreateProject(context.Background(), "proj", t.TempDir()))
	gone := must[*domain.Prompt](t)(f.Agents.CreatePrompt("style", "", "be terse"))
	kept := must[*domain.Prompt](t)(f.Agents.CreatePrompt("tests", "", "write tests"))
	agent := must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "reviewer", "", gone.ID, nil, nil))
	zone := must[*domain.Zone](t)(f.Architecture.CreateZone(p.ID, "api", "", "", []domain.Prompt{{ID: gone.ID}, {ID: kept.ID}}, nil))

	// Zones read rule prompts from the agents context.
	if got := f.Architecture.GetZone(zone.ID); got.Rules[0].Content != "be terse" {
		t.Fatalf("zone rules not hydrated from prompts: %+v", got.Rules)
	}

	if err := f.Agents.DeletePrompt(gone.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Agents.GetAgent(context.Background(), agent.ID); got.PromptID != "" {
		t.Errorf("agent still built on the deleted prompt: %+v", got)
	}
	got := f.Architecture.GetZone(zone.ID)
	if len(got.Rules) != 1 || got.Rules[0].ID != kept.ID {
		t.Errorf("zone rules = %+v, want only %s", got.Rules, kept.ID)
	}
}

func TestDeleteAgent_UnassignsItFromZones(t *testing.T) {
	f := newFixture(t)
	p := must[*domain.Project](t)(f.Projects.CreateProject(context.Background(), "proj", t.TempDir()))
	gone := must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "gone", "", "", nil, nil))
	kept := must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "kept", "", "", nil, nil))
	zone := must[*domain.Zone](t)(f.Architecture.CreateZone(p.ID, "api", "", "", nil, []domain.Agent{{ID: gone.ID}, {ID: kept.ID}}))

	if err := f.Agents.DeleteAgent(context.Background(), gone.ID); err != nil {
		t.Fatal(err)
	}
	got := f.Architecture.GetZone(zone.ID)
	if len(got.AssignedAgents) != 1 || got.AssignedAgents[0].ID != kept.ID {
		t.Fatalf("zone agents = %+v, want only %s", got.AssignedAgents, kept.ID)
	}
}

func TestDeleteSkill_UnlinksAgents(t *testing.T) {
	f := newFixture(t)
	sk := f.skill(t, "shared")
	other := f.skill(t, "kept")
	agent := must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "reviewer", "", "", []string{sk.ID, other.ID}, nil))

	// Agents read skills from the capabilities context.
	if got, _ := f.Agents.GetAgent(context.Background(), agent.ID); len(got.Skills) != 2 {
		t.Fatalf("agent skills not resolved: %+v", got.Skills)
	}

	if err := f.Capabilities.DeleteSkill(sk.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.Agents.GetAgent(context.Background(), agent.ID)
	if !slices.Equal(got.SkillIDs, []string{other.ID}) || len(got.Skills) != 1 {
		t.Fatalf("SkillIDs = %v, want just the surviving skill", got.SkillIDs)
	}
}

// An agent that cannot drop the skill cancels the skill's delete: no agent may
// be left pointing at a skill that is gone.
func TestDeleteSkill_UnlinkFailureCancelsTheDelete(t *testing.T) {
	f := newFixture(t, func(d *Deps) { d.Agents = failingAgentRepo{d.Agents} })
	sk := f.skill(t, "shared")
	must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "reviewer", "", "", []string{sk.ID}, nil))

	if err := f.Capabilities.DeleteSkill(sk.ID); err == nil {
		t.Fatal("DeleteSkill should fail when the agent unlink fails")
	}
	if f.Capabilities.GetSkill(sk.ID) == nil {
		t.Fatal("the skill must survive a failed unlink")
	}
}

func TestMCPServer_UsageAndDeleteUnlinksAgents(t *testing.T) {
	f := newFixture(t)
	srv := must[*domain.MCPServer](t)(f.Capabilities.CreateMCPServer(domain.MCPServerInput{Name: "github", Transport: "stdio", Command: "gh-mcp"}))
	agent := must[*domain.Agent](t)(f.Agents.CreateAgent(context.Background(), "reviewer", "", "", nil, []string{srv.ID}))

	// Capabilities read usage from the agents context.
	got := f.Capabilities.GetMCPServer(srv.ID)
	if got.AgentCount != 1 || !slices.Equal(got.AgentNames, []string{"reviewer"}) {
		t.Fatalf("usage = %d %v, want 1 [reviewer]", got.AgentCount, got.AgentNames)
	}

	if err := f.Capabilities.DeleteMCPServer(srv.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := f.Agents.GetAgent(context.Background(), agent.ID); len(a.MCPServerIDs) != 0 {
		t.Fatalf("agent still links the deleted server: %v", a.MCPServerIDs)
	}
}

func TestPublishRootChange_MigratesPublishedSkills(t *testing.T) {
	f := newFixture(t)
	must[map[string]string](t)(f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/old-root"}))
	sk := f.skill(t, "demo")
	must[*domain.Skill](t)(f.Capabilities.PublishSkill(sk.ID, false))
	f.pub.calls = nil

	must[map[string]string](t)(f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/new-root"}))

	want := []string{"publish:/tmp/new-root/demo force=true", "unpublish:/tmp/old-root/demo"}
	if !slices.Equal(f.pub.calls, want) {
		t.Fatalf("publisher calls = %v, want %v", f.pub.calls, want)
	}
	if got := f.Capabilities.GetSkill(sk.ID); got.PublishedPath != "/tmp/new-root/demo" || got.PublishError != "" {
		t.Fatalf("skill after migration: %+v", got)
	}
}

func TestPublishRootChange_FailedMoveKeepsOldFolder(t *testing.T) {
	f := newFixture(t)
	must[map[string]string](t)(f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/old-root"}))
	sk := f.skill(t, "demo")
	must[*domain.Skill](t)(f.Capabilities.PublishSkill(sk.ID, false))
	f.pub.calls = nil
	f.pub.failWith = errors.New("read-only filesystem")

	if _, err := f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/new-root"}); err != nil {
		t.Fatalf("the setting still changes: %v", err)
	}
	if slices.Contains(f.pub.calls, "unpublish:/tmp/old-root/demo") {
		t.Fatal("the old folder must be left in place when the move failed")
	}
	if got := f.Capabilities.GetSkill(sk.ID); got.PublishError == "" {
		t.Fatal("the failure should be recorded on the skill")
	}
}

// Clearing the root must not touch the filesystem or silently unpublish; it
// only records that the skill is no longer synced.
func TestPublishRootCleared_LeavesFoldersAlone(t *testing.T) {
	f := newFixture(t)
	must[map[string]string](t)(f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/old-root"}))
	sk := f.skill(t, "demo")
	must[*domain.Skill](t)(f.Capabilities.PublishSkill(sk.ID, false))
	f.pub.calls = nil

	must[map[string]string](t)(f.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: ""}))

	if len(f.pub.calls) != 0 {
		t.Fatalf("clearing the root must not touch the filesystem: %v", f.pub.calls)
	}
	got := f.Capabilities.GetSkill(sk.ID)
	if got.PublishedSlug != "demo" || got.PublishError == "" {
		t.Fatalf("skill should stay published and be marked unsynced: %+v", got)
	}
}

// failingAgentRepo fails every Update, leaving reads and creates intact.
type failingAgentRepo struct{ ports.AgentRepository }

func (f failingAgentRepo) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	return nil, &domain.StructuredError{Code: "INTERNAL", Message: "agent store is down"}
}
