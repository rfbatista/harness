package settings

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func newTestService(t *testing.T) (*Service, *eventbus.Bus) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	return NewService(sqlite.NewSettingsRepository(db), bus), bus
}

func TestUpdateSettings_MergesKeys(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "/tmp/pub-root"}); err != nil {
		t.Fatal(err)
	}
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
	if svc.Setting("other.key") != "v" || svc.Setting("unset") != "" {
		t.Fatalf("Setting reads wrong: %q %q", svc.Setting("other.key"), svc.Setting("unset"))
	}
}

func TestUpdateSettings_RejectsRelativeRootBeforeStoringAnything(t *testing.T) {
	svc, bus := newTestService(t)
	announced := false
	ports.On(bus, func(context.Context, domain.SettingsChanged) error { announced = true; return nil })

	_, err := svc.UpdateSettings(map[string]string{domain.SettingSkillsPublishRoot: "relative/dir", "other.key": "v"})
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %v", err)
	}
	if svc.Setting(domain.SettingSkillsPublishRoot) != "" || svc.Setting("other.key") != "" {
		t.Fatal("a rejected update must store nothing")
	}
	if announced {
		t.Fatal("a rejected update must announce nothing")
	}
}

func TestUpdateSettings_TakesOnlyACavemanIntensity(t *testing.T) {
	svc, _ := newTestService(t)
	for _, v := range []string{"lite", "full", "ultra"} {
		if _, err := svc.UpdateSettings(map[string]string{domain.SettingCavemanIntensity: v}); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	for _, v := range []string{"", "max", "Full"} {
		_, err := svc.UpdateSettings(map[string]string{domain.SettingCavemanIntensity: v})
		var se *domain.StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
			t.Errorf("%q: expected INVALID_INPUT, got %v", v, err)
		}
	}
	if got := svc.Setting(domain.SettingCavemanIntensity); got != "ultra" {
		t.Errorf("intensity = %q, want ultra", got)
	}
}

func TestUpdateSettings_TakesOnlyTrueOrFalseForAgentsMovingDocuments(t *testing.T) {
	svc, _ := newTestService(t)
	if got := svc.Setting(domain.SettingAgentsMoveDocuments); got != "" {
		t.Fatalf("unset setting = %q, want empty", got)
	}
	for _, v := range []string{"true", "false", ""} {
		if _, err := svc.UpdateSettings(map[string]string{domain.SettingAgentsMoveDocuments: v}); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	for _, v := range []string{"yes", "True", "1"} {
		_, err := svc.UpdateSettings(map[string]string{domain.SettingAgentsMoveDocuments: v})
		var se *domain.StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
			t.Errorf("%q: expected INVALID_INPUT, got %v", v, err)
		}
	}
}

func TestUpdateSettings_AnnouncesOnlyChanges(t *testing.T) {
	svc, bus := newTestService(t)
	var got []domain.SettingsChanged
	ports.On(bus, func(_ context.Context, ev domain.SettingsChanged) error { got = append(got, ev); return nil })

	if _, err := svc.UpdateSettings(map[string]string{"k": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateSettings(map[string]string{"k": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateSettings(map[string]string{"k": "b"}); err != nil {
		t.Fatal(err)
	}
	want := []domain.SettingsChanged{{Key: "k", Old: "", New: "a"}, {Key: "k", Old: "a", New: "b"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("announced %+v, want %+v", got, want)
	}
}

func TestUpdateSettings_ReportsSubscriberFailureAfterStoring(t *testing.T) {
	svc, bus := newTestService(t)
	ports.On(bus, func(context.Context, domain.SettingsChanged) error { return errors.New("migration failed") })

	if _, err := svc.UpdateSettings(map[string]string{"k": "v"}); err == nil {
		t.Fatal("a subscriber failure must be reported")
	}
	if svc.Setting("k") != "v" {
		t.Fatal("the setting did change and must stay stored")
	}
}

func TestUnavailableWithoutRepository(t *testing.T) {
	svc := NewService(nil, nil)
	if all, err := svc.GetSettings(); err != nil || len(all) != 0 {
		t.Fatalf("GetSettings = %v, %v", all, err)
	}
	if _, err := svc.UpdateSettings(map[string]string{"k": "v"}); err == nil {
		t.Fatal("UpdateSettings without a repository must fail")
	}
}

// modelMigrationService is a service over a store holding the given rows, as
// an upgraded server finds it at boot.
func modelMigrationService(t *testing.T, rows map[string]string) (*Service, ports.SettingsRepository) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewSettingsRepository(db)
	for k, v := range rows {
		if err := repo.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return NewService(repo, nil), repo
}

func TestMigrateModelSettings_MovesTheSavedValuesToTheirModes(t *testing.T) {
	svc, _ := modelMigrationService(t, map[string]string{
		"models.claude.architect":      "fable",
		"models.claude.implementation": "haiku",
		"other.key":                    "v",
	})
	if err := svc.MigrateModelSettings(); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.GetSettings()
	want := map[string]string{
		"models.claude.mode.architect": "fable",
		"models.claude.mode.implement": "haiku",
		"other.key":                    "v",
	}
	if len(all) != len(want) {
		t.Fatalf("settings = %v, want %v", all, want)
	}
	for k, v := range want {
		if all[k] != v {
			t.Errorf("settings[%q] = %q, want %q (all: %v)", k, all[k], v, all)
		}
	}
	// A second boot changes nothing.
	if err := svc.MigrateModelSettings(); err != nil {
		t.Fatal(err)
	}
	again, _ := svc.GetSettings()
	if len(again) != len(want) || again["models.claude.mode.architect"] != "fable" {
		t.Errorf("settings after a second migration = %v, want %v", again, want)
	}
}

func TestMigrateModelSettings_NeverOverwritesANewKeyButStillDeletesTheOldOne(t *testing.T) {
	svc, _ := modelMigrationService(t, map[string]string{
		"models.claude.architect":      "fable",
		"models.claude.mode.architect": "sonnet",
	})
	if err := svc.MigrateModelSettings(); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.GetSettings()
	if len(all) != 1 || all["models.claude.mode.architect"] != "sonnet" {
		t.Errorf("settings = %v, want only the architect mode kept at sonnet", all)
	}
}

func TestMigrateModelSettings_ChangesNothingWithoutOldKeys(t *testing.T) {
	svc, _ := modelMigrationService(t, map[string]string{"models.claude.mode.design": "haiku"})
	if err := svc.MigrateModelSettings(); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.GetSettings()
	if len(all) != 1 || all["models.claude.mode.design"] != "haiku" {
		t.Errorf("settings = %v, want the design row alone", all)
	}
}
