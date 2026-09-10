package orchestration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The tree-writing itself is covered by harnesskit; what matters here is the
// shape this adapter hands to the Claude CLI.
func TestResolveSkillDirs_ReturnsOnePluginDir(t *testing.T) {
	dirs, cleanup, err := resolveSkillDirs([]domain.Skill{{
		Name: "Code Review",
		Files: []domain.SkillFile{
			{Path: domain.SkillFileName, Content: domain.SkillMarkdownFor("code-review", "reviews code", "Do the review.")},
			{Path: "scripts/run.py", Content: "print('hi')"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if len(dirs) != 1 {
		t.Fatalf("dirs = %v, want exactly one plugin dir", dirs)
	}
	if _, err := os.Stat(filepath.Join(dirs[0], ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("plugin.json missing: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dirs[0], "skills", "code-review", domain.SkillFileName))
	if err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(b), "Do the review.") {
		t.Fatalf("SKILL.md content wrong:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dirs[0], "skills", "code-review", "scripts", "run.py")); err != nil {
		t.Fatalf("nested script missing: %v", err)
	}
}

// The dirs slice is expanded into repeated --plugin-dir flags, so "nothing to
// materialize" has to be a nil slice. A []string{""} would hand the CLI an
// empty flag value.
func TestResolveSkillDirs_NothingToWriteYieldsNilNotEmptyString(t *testing.T) {
	cases := map[string][]domain.Skill{
		"no skills":           nil,
		"skill with no files": {{Name: "empty"}},
		"skill with no usable name": {{
			Name:  "---",
			Files: []domain.SkillFile{{Path: "notes.md", Content: "no root SKILL.md"}},
		}},
	}
	for name, skills := range cases {
		t.Run(name, func(t *testing.T) {
			dirs, cleanup, err := resolveSkillDirs(skills)
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			defer cleanup()
			if dirs != nil {
				t.Fatalf("dirs = %#v, want nil", dirs)
			}
		})
	}
}

// A session's cleanup runs when the session ends; it must actually remove the
// temp tree, and must be safe even when nothing was written.
func TestResolveSkillDirs_CleanupRemovesTheTree(t *testing.T) {
	dirs, cleanup, err := resolveSkillDirs([]domain.Skill{{
		Name:  "s",
		Files: []domain.SkillFile{{Path: domain.SkillFileName, Content: domain.SkillMarkdownFor("s", "d", "b")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("plugin dir should be gone after cleanup, stat err = %v", err)
	}

	_, noopCleanup, err := resolveSkillDirs(nil)
	if err != nil {
		t.Fatal(err)
	}
	noopCleanup() // must not panic
}

// Records predating the file-tree model still have to reach the agent.
func TestResolveSkillDirs_LegacyPathFallback(t *testing.T) {
	src := filepath.Join(t.TempDir(), "from-disk")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := domain.SkillMarkdownFor("from-disk", "Loaded from disk", "From disk.")
	if err := os.WriteFile(filepath.Join(src, domain.SkillFileName), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	dirs, cleanup, err := resolveSkillDirs([]domain.Skill{{Name: "From Disk", Path: src}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(dirs) != 1 {
		t.Fatalf("dirs = %v, want one", dirs)
	}
	b, err := os.ReadFile(filepath.Join(dirs[0], "skills", "from-disk", domain.SkillFileName))
	if err != nil {
		t.Fatalf("materialized skill missing: %v", err)
	}
	if !strings.Contains(string(b), "From disk.") {
		t.Fatalf("unexpected content: %s", b)
	}
}
