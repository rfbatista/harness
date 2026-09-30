package tooling

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/mcptools"
	"github.com/rfbatista/harnesskit/skillfs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/domain"
)

// settingsFixture is the settings and capabilities contexts together: the
// settings tools edit the publish root the skill tools publish under.
type settingsFixture struct {
	Settings *settings.Service
	Caps     *capabilities.Service
}

func newSettingsToolService(t *testing.T) settingsFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st := settings.NewService(sqlite.NewSettingsRepository(db), nil)
	caps := capabilities.NewService(capabilities.Deps{
		Skills:    sqlite.NewSkillRepository(db),
		Settings:  st,
		Publisher: skillfs.NewPublisher(),
	})
	return settingsFixture{Settings: st, Caps: caps}
}

func toolByName(t *testing.T, tools []domain.Tool, name string) domain.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered", name)
	return domain.Tool{}
}

func TestSettingsTools_UpdateThenGet(t *testing.T) {
	svc := newSettingsToolService(t)
	tools := SettingsTools(svc.Settings)
	root := t.TempDir()

	update := toolByName(t, tools, "update_settings")
	if _, err := update.Handler(context.Background(), map[string]any{
		"settings": map[string]any{domain.SettingSkillsPublishRoot: root},
	}); err != nil {
		t.Fatal(err)
	}

	get := toolByName(t, tools, "get_settings")
	out, err := get.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	settings, ok := result["settings"].(map[string]string)
	if !ok || settings[domain.SettingSkillsPublishRoot] != root {
		t.Fatalf("settings = %#v", result["settings"])
	}
}

func TestSkillTools_PublishAndUnpublish(t *testing.T) {
	svc := newSettingsToolService(t)
	root := t.TempDir()
	if _, err := svc.Settings.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: root}); err != nil {
		t.Fatal(err)
	}
	skill, err := svc.Caps.CreateSkill(domain.SkillInput{
		Name:  "demo",
		Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# demo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The tool group comes from harnesskit; this asserts it reaches the real
	// SQLite repository and the settings-backed publish root through the settings context.
	tools := mcptools.SkillTools(svc.Caps.SkillService())

	publish := toolByName(t, tools, "publish_skill")
	if _, err := publish.Handler(context.Background(), map[string]any{"skill_id": skill.ID}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Caps.GetSkill(skill.ID); !got.IsPublished() {
		t.Fatal("publish_skill did not publish")
	}

	unpublish := toolByName(t, tools, "unpublish_skill")
	if _, err := unpublish.Handler(context.Background(), map[string]any{"skill_id": skill.ID}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Caps.GetSkill(skill.ID); got.IsPublished() {
		t.Fatal("unpublish_skill did not unpublish")
	}

	if _, err := publish.Handler(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected INVALID_INPUT without skill_id")
	}
}
