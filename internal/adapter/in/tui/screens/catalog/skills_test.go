package catalog

import (
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/domain"
)

func skillsFixture() (*backend.Fake, Model) {
	f := backend.NewFake()
	f.Skills = []*domain.Skill{{
		ID: "k1", Name: "tdd-workflow", Description: "red green refactor",
		Files: []domain.SkillFile{
			{Path: "SKILL.md", Content: "---\nname: tdd-workflow\n---\n# TDD\n\nWrite the test first."},
			{Path: "refs/cheatsheet.md", Content: "# Cheatsheet"},
			{Path: "scripts", Dir: true},
		},
	}}
	m := withSnap(New(ctx(), Skills, f), f)
	return f, m
}

func TestSkillsNewCreatesSkillWithSkillMD(t *testing.T) {
	f, m := skillsFixture()
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "docs-writer")
	m, _ = press(m, tab())
	m = typeKeys(m, "writes docs")
	m, _ = m.Update(editorDone("body", "# Docs\n\nHow to write docs."))
	_, root := press(m, enter())
	if len(f.Skills) != 2 || !hasRefresh(root) {
		t.Fatalf("skill not created: %+v", f.Skills)
	}
	s := f.Skills[1]
	if s.Name != "docs-writer" || len(s.Files) != 1 || s.Files[0].Path != "SKILL.md" {
		t.Fatalf("skill shape: %+v", s)
	}
	if !strings.Contains(s.Files[0].Content, "name: docs-writer") || !strings.Contains(s.Files[0].Content, "description: writes docs") || !strings.Contains(s.Files[0].Content, "# Docs") {
		t.Fatalf("SKILL.md should carry frontmatter and body:\n%s", s.Files[0].Content)
	}
}

func TestSkillsEditUpdatesMetadata(t *testing.T) {
	f, m := skillsFixture()
	m, _ = press(m, ch('e'))
	if !strings.Contains(m.View(), "tdd-workflow") {
		t.Fatalf("edit prefilled:\n%s", m.View())
	}
	m, _ = press(m, tab()) // description
	m, _ = press(m, tab()) // license
	m = typeKeys(m, "MIT")
	_, _ = press(m, enter())
	if f.Skills[0].License != "MIT" || f.Skills[0].Name != "tdd-workflow" {
		t.Fatalf("metadata not updated: %+v", f.Skills[0])
	}
}

func TestSkillsImportValidatesThenPreviewsThenImports(t *testing.T) {
	f, m := skillsFixture()
	f.ValidatePath = func(path string) (*domain.SkillPathValidation, error) {
		if path == "/bad" {
			return &domain.SkillPathValidation{Valid: false, Validation: []domain.ValidationIssue{{Level: "error", Message: "SKILL.md missing"}}}, nil
		}
		return &domain.SkillPathValidation{Valid: true, SkillRoot: path, Preview: &domain.Skill{Name: "imported-skill", Files: []domain.SkillFile{{Path: "SKILL.md"}, {Path: "a.md"}}}}, nil
	}
	m, _ = press(m, ch('i'))
	m = typeKeys(m, "/bad")
	m, _ = press(m, enter())
	if !strings.Contains(m.View(), "SKILL.md missing") || !m.Capturing() {
		t.Fatalf("invalid path should stay on the form with the issue:\n%s", m.View())
	}
	m, _ = press(m, esc())
	m, _ = press(m, ch('i'))
	m = typeKeys(m, "/good")
	m, _ = press(m, enter())
	v := m.View()
	if !strings.Contains(v, "imported-skill") || !strings.Contains(v, "2 files") {
		t.Fatalf("preview confirm expected:\n%s", v)
	}
	_, root := press(m, ch('y'))
	if len(f.Skills) != 2 || f.Skills[1].Name != "imported-skill" || !hasRefresh(root) {
		t.Fatalf("import did not happen: %+v", f.Skills)
	}
}

func TestSkillsPublishConflictOffersForce(t *testing.T) {
	f, m := skillsFixture()
	f.Skills[0].PublishedPath = "/published/tdd-workflow"
	m, _ = press(m, ch('p'))
	v := m.View()
	if !strings.Contains(v, "already exists") || !m.Capturing() {
		t.Fatalf("conflict should ask to force:\n%s", v)
	}
	_, root := press(m, ch('y'))
	if !hasRefresh(root) {
		t.Fatal("force publish should refresh")
	}
}

func TestSkillsPublishAndUnpublish(t *testing.T) {
	f, m := skillsFixture()
	_, root := press(m, ch('p'))
	if f.Skills[0].PublishedPath == "" || !hasRefresh(root) {
		t.Fatalf("publish: %+v", f.Skills[0])
	}
	_, _ = press(m, ch('u'))
	if f.Skills[0].PublishedPath != "" {
		t.Fatalf("unpublish: %+v", f.Skills[0])
	}
}

func TestSkillsDeleteConfirms(t *testing.T) {
	f, m := skillsFixture()
	m, _ = press(m, ch('D'))
	_, _ = press(m, ch('y'))
	if len(f.Skills) != 0 {
		t.Fatalf("skill should be deleted: %+v", f.Skills)
	}
}

func TestSkillDetailBrowsesFilesWithPreview(t *testing.T) {
	_, m := skillsFixture()
	m, _ = press(m, enter())
	v := plain(m.View())
	for _, want := range []string{"SKILL.md", "refs/cheatsheet.md", "scripts/", "Write the test first"} {
		if !strings.Contains(v, want) {
			t.Fatalf("browser missing %q:\n%s", want, v)
		}
	}
	m, _ = press(m, ch('j'))
	if !strings.Contains(m.View(), "Cheatsheet") {
		t.Fatalf("preview should follow the cursor:\n%s", m.View())
	}
	m, _ = press(m, esc())
	if strings.Contains(m.View(), "Cheatsheet") {
		t.Fatal("esc should leave the browser")
	}
}

func TestSkillDetailEditWritesFileBack(t *testing.T) {
	f, m := skillsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('j'))
	m, _ = press(m, ch('e')) // opens $EDITOR; simulate its result
	m, cmd := m.Update(editorDone("file:refs/cheatsheet.md", "# Cheatsheet v2"))
	_, _ = drive(m, cmd)
	if f.Skills[0].Files[1].Content != "# Cheatsheet v2" {
		t.Fatalf("file not written: %+v", f.Skills[0].Files[1])
	}
}

func TestSkillDetailAddRenameDeleteFiles(t *testing.T) {
	f, m := skillsFixture()
	m, _ = press(m, enter())
	m, _ = press(m, ch('a'))
	m = typeKeys(m, "notes.md")
	m, _ = press(m, enter())
	if len(f.Skills[0].Files) != 4 || f.Skills[0].Files[3].Path != "notes.md" {
		t.Fatalf("file not added: %+v", f.Skills[0].Files)
	}
	m = withSnap(m, f)
	m, _ = press(m, ch('A'))
	m = typeKeys(m, "assets")
	m, _ = press(m, enter())
	if len(f.Skills[0].Files) != 5 || !f.Skills[0].Files[4].Dir {
		t.Fatalf("folder not added: %+v", f.Skills[0].Files)
	}
	m = withSnap(m, f)
	// sorted: SKILL.md, assets/, notes.md, refs/cheatsheet.md, scripts/
	m, _ = press(m, ch('j'))
	m, _ = press(m, ch('j'))
	m, _ = press(m, ch('j'))
	m, _ = press(m, ch('R'))
	if !strings.Contains(m.View(), "refs/cheatsheet.md") {
		t.Fatalf("rename prefilled:\n%s", m.View())
	}
	m = typeKeys(m, ".bak")
	m, _ = press(m, enter())
	if f.Skills[0].Files[1].Path != "refs/cheatsheet.md.bak" { // Files keeps insertion order
		t.Fatalf("rename: %+v", f.Skills[0].Files[1])
	}
	m = withSnap(m, f)
	m, _ = press(m, ch('D'))
	_, _ = press(m, ch('y'))
	if len(f.Skills[0].Files) != 4 {
		t.Fatalf("delete file: %+v", f.Skills[0].Files)
	}
}
