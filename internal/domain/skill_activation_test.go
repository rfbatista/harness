package domain

import (
	"strings"
	"testing"
)

func TestParseSkillActivation(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
		paths    []string
		types    []string
	}{
		{"no metadata", nil, nil, nil},
		{"unrelated metadata", map[string]string{"author": "me"}, nil, nil},
		{"path patterns", map[string]string{"activation_paths": "web/**/*.templ, internal/**"}, []string{"web/**/*.templ", "internal/**"}, nil},
		{"task types", map[string]string{"activation_task_types": "implement,review"}, nil, []string{"implement", "review"}},
		{"both", map[string]string{"activation_paths": "docs/**", "activation_task_types": "design"}, []string{"docs/**"}, []string{"design"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSkillActivation(c.metadata)
			if err != nil {
				t.Fatalf("ParseSkillActivation: %v", err)
			}
			if strings.Join(got.Paths, "|") != strings.Join(c.paths, "|") || strings.Join(got.TaskTypes, "|") != strings.Join(c.types, "|") {
				t.Fatalf("got %+v, want paths %v types %v", got, c.paths, c.types)
			}
			if got.Empty() != (c.paths == nil && c.types == nil) {
				t.Fatalf("Empty() = %v for %+v", got.Empty(), got)
			}
		})
	}
}

func TestParseSkillActivationRefusesAnInvalidRule(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
		want     string
	}{
		{"unparsable glob", map[string]string{"activation_paths": "web/[a-"}, "activation_paths"},
		{"absolute glob", map[string]string{"activation_paths": "/etc/**"}, "activation_paths"},
		{"parent glob", map[string]string{"activation_paths": "../x"}, "activation_paths"},
		{"empty value", map[string]string{"activation_paths": " , "}, "activation_paths"},
		{"unknown task type", map[string]string{"activation_task_types": "deploy"}, "deploy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseSkillActivation(c.metadata)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one naming %q", err, c.want)
			}
		})
	}
}

func TestSkillActivationMatches(t *testing.T) {
	a, err := ParseSkillActivation(map[string]string{"activation_paths": "web/**/*.templ, internal/domain/*.go, README.md", "activation_task_types": "implement"})
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		"web/a/b/page.templ":      true,
		"web/page.templ":          true,
		"internal/domain/task.go": true,
		"internal/domain/x/y.go":  false,
		"README.md":               true,
		"docs/README.md":          false,
		"web/page.go":             false,
	} {
		if got := a.MatchesPath(file); got != want {
			t.Errorf("MatchesPath(%q) = %v, want %v", file, got, want)
		}
	}
	if !a.MatchesTaskType(SessionModeImplement) || a.MatchesTaskType(SessionModeReview) {
		t.Error("MatchesTaskType: implement should match, review should not")
	}
}
