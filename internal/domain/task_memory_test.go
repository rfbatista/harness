package domain

import (
	"strings"
	"testing"
)

func TestNewTaskMemoryHasTheFixedSections(t *testing.T) {
	m := NewTaskMemory()
	if !strings.Contains(m.Plan, "## Steps") {
		t.Errorf("plan.md = %q", m.Plan)
	}
	got := ProgressSections(m.Progress)
	for _, name := range ProgressSectionNames {
		if _, ok := got[name]; !ok {
			t.Errorf("progress.md has no %q section: %q", name, m.Progress)
		}
	}
}

func TestProgressUnchangedComparesEverySection(t *testing.T) {
	base := "# Progress\n\n## Done\n\n- scaffolded\n\n## Failed and why\n\n## Next step\n\nwrite tests\n\n## Open questions\n"
	cases := []struct {
		name string
		now  string
		want bool
	}{
		{"identical", base, true},
		{"only whitespace and line endings differ", strings.ReplaceAll(base, "\n", "\r\n") + "\n\n", true},
		{"a section gained a line", strings.Replace(base, "- scaffolded", "- scaffolded\n- tests", 1), false},
		{"next step rewritten", strings.Replace(base, "write tests", "ship it", 1), false},
		{"a section was removed", strings.Replace(base, "## Open questions\n", "", 1), false},
		{"text before the first section is not a section", "intro\n" + base, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProgressUnchanged(base, c.now); got != c.want {
				t.Fatalf("ProgressUnchanged = %v, want %v", got, c.want)
			}
		})
	}
}
