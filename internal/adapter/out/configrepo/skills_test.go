package configrepo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/skill"
)

func writeSkill(t *testing.T, skillsRoot, name, skillMD string) {
	t.Helper()
	dir := filepath.Join(skillsRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

const tddSkillMD = "---\nname: tdd\ndescription: Write tests first.\n---\n\n# TDD\n"

func TestSkillStore_ListReadsEverySkillDir(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "tdd", tddSkillMD)
	writeSkill(t, root, "git-commit", "---\nname: git-commit\ndescription: Commit well.\n---\n\nbody\n")

	store := NewSkillStore(root)
	got := store.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(got), got)
	}
	if got[0].Name != "git-commit" || got[1].Name != "tdd" {
		t.Fatalf("expected alphabetical order, got %s, %s", got[0].Name, got[1].Name)
	}
}

func TestSkillStore_GetReturnsMatchingSkillWithFilesAndFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "tdd", tddSkillMD)

	store := NewSkillStore(root)
	got := store.Get("cfg:tdd")
	if got == nil {
		t.Fatal("expected a skill, got nil")
	}
	if got.ID != "cfg:tdd" || got.Name != "tdd" || got.Description != "Write tests first." {
		t.Fatalf("got %+v", got)
	}
	if len(got.Files) == 0 {
		t.Fatal("expected the skill's file tree to be populated")
	}
}

func TestSkillStore_GetReturnsNilForNonConfigID(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "tdd", tddSkillMD)
	if got := NewSkillStore(root).Get("db-generated-uuid"); got != nil {
		t.Fatalf("expected nil for a non-\"cfg:\" id, got %+v", got)
	}
}

func TestSkillStore_GetReturnsNilForUnknownSkill(t *testing.T) {
	root := t.TempDir()
	if got := NewSkillStore(root).Get("cfg:nope"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestSkillStore_ListReflectsLiveEditsAcrossCalls(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "tdd", tddSkillMD)
	store := NewSkillStore(root)

	first := store.List()
	if len(first) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(first))
	}

	writeSkill(t, root, "git-commit", "---\nname: git-commit\ndescription: x\n---\n\nbody\n")
	second := store.List()
	if len(second) != 2 {
		t.Fatalf("expected the second read to see the newly added skill, got %d", len(second))
	}
}

func TestSkillStore_WritesAreReadOnly(t *testing.T) {
	store := NewSkillStore(t.TempDir())
	if _, err := store.Create(skill.Input{}); err == nil {
		t.Fatal("expected Create to fail")
	}
	if _, err := store.Update("cfg:tdd", skill.Input{}); err == nil {
		t.Fatal("expected Update to fail")
	}
	if err := store.Delete("cfg:tdd"); err == nil {
		t.Fatal("expected Delete to fail")
	}
}
