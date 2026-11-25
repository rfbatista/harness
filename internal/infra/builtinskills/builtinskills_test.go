package builtinskills

import (
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func TestAll_ListsTheBuiltInSkillsWithTheirModes(t *testing.T) {
	got := map[string][]string{}
	for _, s := range All() {
		got[s.Name] = s.Modes
		if s.Description == "" {
			t.Errorf("%s has no description", s.Name)
		}
		if len(s.Files) == 0 {
			t.Errorf("%s has no files", s.Name)
		}
	}
	want := map[string][]string{
		"task-architecture": {"architect"}, "task-grill-with-docs": {"architect"}, "task-to-spec": {"architect"},
		"task-run-app":        {"operate"},
		"task-implement-spec": {"implement"}, "task-tdd": {"implement", "test"}, "task-code-review": {"implement", "review"},
		"design-artifacts": {"design"}, "task-product": {"product"},
		"task-review": {"review"}, "task-test": {"test"}, "task-qa": {"qa"}, "task-operate": {"operate"},
		"create-verification-skill": {"architect"}, "maintain-verification-skill": {"architect"},
	}
	if len(got) != len(want) {
		t.Fatalf("skills = %v, want %d", got, len(want))
	}
	for name, modes := range want {
		if !slices.Equal(got[name], modes) {
			t.Errorf("%s modes = %v, want %v", name, got[name], modes)
		}
	}
}

func TestForMode_KeepsTheModesOrder(t *testing.T) {
	var names []string
	for _, s := range ForMode(domain.SessionModeArchitect) {
		names = append(names, s.Name)
	}
	want := []string{"task-architecture", "task-grill-with-docs", "task-to-spec", "create-verification-skill", "maintain-verification-skill"}
	if !slices.Equal(names, want) {
		t.Fatalf("architect skills = %v, want %v", names, want)
	}
	for mode, want := range map[domain.SessionMode][]string{
		domain.SessionModeReview:  {"task-review", "task-code-review"},
		domain.SessionModeTest:    {"task-test", "task-tdd"},
		domain.SessionModeQA:      {"task-qa"},
		domain.SessionModeOperate: {"task-operate", "task-run-app"},
	} {
		var got []string
		for _, s := range ForMode(mode) {
			got = append(got, s.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s skills = %v, want %v", mode, got, want)
		}
	}
	if len(ForMode(domain.SessionMode("none"))) != 0 {
		t.Fatal("a mode with no skills yields none")
	}
}

func TestGet_ServesTheSameSetEveryTime(t *testing.T) {
	a, ok := Get("task-run-app")
	if !ok {
		t.Fatal("task-run-app is built in")
	}
	b, _ := Get("task-run-app")
	if &a.Files[0] != &b.Files[0] {
		t.Fatal("the embedded files are read once, not per ask")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown name must not be built in")
	}
}

// Every upstream file of the skill is embedded with it, the MIT notice sits in
// its folder, and the person's decision holds: no disable-model-invocation.
func TestGet_CreateVerificationSkillShipsEveryUpstreamFileAndTheLicence(t *testing.T) {
	s, ok := Get("create-verification-skill")
	if !ok {
		t.Fatal("create-verification-skill is built in")
	}
	files := map[string]string{}
	for _, f := range s.Files {
		files[f.Path] = f.Content
	}
	for _, p := range []string{
		"SKILL.md", "LICENSE",
		"references/feature-map-example/README.md",
		"references/feature-map-example/create-note.md",
		"references/feature-map-example/search.md",
	} {
		if files[p] == "" {
			t.Errorf("%s is not embedded", p)
		}
	}
	if !strings.Contains(files["LICENSE"], "MIT License") || !strings.Contains(files["LICENSE"], "Lauren Tan") {
		t.Error("LICENSE is not the upstream MIT notice")
	}
	body := files["SKILL.md"]
	if !strings.Contains(body, "name: create-verification-skill") || s.Description == "" {
		t.Error("SKILL.md lacks frontmatter name and description")
	}
	if strings.Contains(body, "disable-model-invocation") {
		t.Error("the architect may load the skill after the person agrees: no disable-model-invocation")
	}
	if !strings.Contains(body, "bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7") || !strings.Contains(body, "cursor/plugins") {
		t.Error("SKILL.md does not name the upstream repository and commit")
	}
}

func TestGet_MaintainVerificationSkillShipsWithTheLicenceAndTheUpstreamSource(t *testing.T) {
	s, ok := Get("maintain-verification-skill")
	if !ok {
		t.Fatal("maintain-verification-skill is built in")
	}
	files := map[string]string{}
	for _, f := range s.Files {
		files[f.Path] = f.Content
	}
	if !strings.Contains(files["LICENSE"], "MIT License") {
		t.Error("LICENSE is not the upstream MIT notice")
	}
	body := files["SKILL.md"]
	if !strings.Contains(body, "name: maintain-verification-skill") || s.Description == "" {
		t.Error("SKILL.md lacks frontmatter name and description")
	}
	if strings.Contains(body, "disable-model-invocation") {
		t.Error("no disable-model-invocation")
	}
	if !strings.Contains(body, "bf0c7cd80abdbf7fe47b634978a7d1b4071f71a7") || !strings.Contains(body, "propose_instruction_files") {
		t.Error("SKILL.md lacks the upstream commit or the update proposal")
	}
}
