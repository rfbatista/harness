package builtinskills

import (
	"slices"
	"testing"

	"operators-mcp/internal/domain"
)

func TestAll_ListsTheNineBuiltInSkillsWithTheirModes(t *testing.T) {
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
		"task-run-app":        {"architect"},
		"task-implement-spec": {"implement"}, "task-tdd": {"implement"}, "task-code-review": {"implement"},
		"design-artifacts": {"design"}, "task-product": {"product"},
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
	want := []string{"task-architecture", "task-grill-with-docs", "task-to-spec", "task-run-app"}
	if !slices.Equal(names, want) {
		t.Fatalf("architect skills = %v, want %v", names, want)
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
