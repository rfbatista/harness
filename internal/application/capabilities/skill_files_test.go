package capabilities

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

func newSkillTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	skills := sqlite.NewSkillRepository(db)
	return NewService(Deps{Skills: skills})
}

func TestCreateSkill_SynthesizesRootSkillFile(t *testing.T) {
	svc := newSkillTestService(t)
	skill, err := svc.CreateSkill(domain.SkillInput{Name: "my-skill", Description: "does things"})
	if err != nil {
		t.Fatal(err)
	}
	if len(skill.Files) != 1 || skill.Files[0].Path != domain.SkillFileName {
		t.Fatalf("expected a single SKILL.md, got %+v", skill.Files)
	}
	if skill.Name != "my-skill" || skill.Description != "does things" {
		t.Fatalf("derived metadata wrong: %+v", skill)
	}
}

func TestSkillFileCRUD_RoundTrip(t *testing.T) {
	svc := newSkillTestService(t)
	skill, err := svc.CreateSkill(domain.SkillInput{
		Name: "tree-skill",
		Files: []domain.SkillFile{
			{Path: "SKILL.md", Content: "---\nname: tree-skill\ndescription: a tree\n---\n\nbody"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := skill.ID

	// add a nested file
	if _, err := svc.PutSkillFile(id, domain.SkillFile{Path: "scripts/run.py", Content: "print(1)"}); err != nil {
		t.Fatal(err)
	}
	// add another under the same folder
	if _, err := svc.PutSkillFile(id, domain.SkillFile{Path: "scripts/util.py", Content: "x=1"}); err != nil {
		t.Fatal(err)
	}

	// rename the folder -> descendants move
	if _, err := svc.RenameSkillFile(id, "scripts", "lib"); err != nil {
		t.Fatal(err)
	}
	files, err := svc.ListSkillFiles(id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	if !got["lib/run.py"] || !got["lib/util.py"] || got["scripts/run.py"] {
		t.Fatalf("folder rename did not move descendants: %+v", files)
	}

	// delete the folder -> both descendants gone
	if _, err := svc.DeleteSkillFile(id, "lib"); err != nil {
		t.Fatal(err)
	}
	files, _ = svc.ListSkillFiles(id)
	if len(files) != 1 || files[0].Path != domain.SkillFileName {
		t.Fatalf("folder delete left files: %+v", files)
	}
}

func TestDeleteSkillFile_RejectsRoot(t *testing.T) {
	svc := newSkillTestService(t)
	skill, err := svc.CreateSkill(domain.SkillInput{Name: "root-skill", Description: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteSkillFile(skill.ID, "SKILL.md"); err == nil {
		t.Fatal("expected deleting root SKILL.md to fail")
	}
}

func TestUpdateSkill_MetadataOnlyKeepsTree(t *testing.T) {
	svc := newSkillTestService(t)
	skill, err := svc.CreateSkill(domain.SkillInput{
		Name: "keep-tree",
		Files: []domain.SkillFile{
			{Path: "SKILL.md", Content: "---\nname: keep-tree\ndescription: d\n---\n\nb"},
			{Path: "notes.md", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateSkill(skill.ID, domain.SkillInput{License: "MIT"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Files) != 2 {
		t.Fatalf("metadata-only update dropped files: %+v", updated.Files)
	}
	if updated.License != "MIT" {
		t.Fatalf("license not updated: %+v", updated)
	}
}
