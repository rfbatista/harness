package sqlite

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestSettingsRepository_RoundTrip(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewSettingsRepository(db)

	value, err := repo.Get(domain.SettingSkillsPublishRoot)
	if err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("unset key should read as empty, got %q", value)
	}

	if err := repo.Set(domain.SettingSkillsPublishRoot, "/tmp/skills"); err != nil {
		t.Fatal(err)
	}
	if value, err = repo.Get(domain.SettingSkillsPublishRoot); err != nil || value != "/tmp/skills" {
		t.Fatalf("Get = %q, %v", value, err)
	}

	if err := repo.Set(domain.SettingSkillsPublishRoot, "/tmp/other"); err != nil {
		t.Fatal(err)
	}
	if value, err = repo.Get(domain.SettingSkillsPublishRoot); err != nil || value != "/tmp/other" {
		t.Fatalf("Set should overwrite, got %q, %v", value, err)
	}

	if err := repo.Set("other.key", "x"); err != nil {
		t.Fatal(err)
	}
	all, err := repo.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all["other.key"] != "x" || all[domain.SettingSkillsPublishRoot] != "/tmp/other" {
		t.Fatalf("All = %v", all)
	}
}
