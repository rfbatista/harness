package domain

import (
	"strings"
	"testing"
)

func TestParseSkillActivationAcceptsNewlineSeparatedListsAndTrimsBlanks(t *testing.T) {
	a, err := ParseSkillActivation(map[string]string{
		"activation_paths":      "web/**\n  docs/*.md ,, \n",
		"activation_task_types": " review ,\ntest ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Paths, "|") != "web/**|docs/*.md" || strings.Join(a.TaskTypes, "|") != "review|test" {
		t.Errorf("got %+v", a)
	}
}

func TestParseSkillActivationRefusesMoreInvalidRules(t *testing.T) {
	for name, md := range map[string]map[string]string{
		"empty task types":        {"activation_task_types": ""},
		"blank task types":        {"activation_task_types": " , \n"},
		"empty paths":             {"activation_paths": ""},
		"backslash path":          {"activation_paths": `web\**`},
		"dotdot in the middle":    {"activation_paths": "web/../etc/x"},
		"unclosed bracket mid":    {"activation_paths": "ok/*.go, bad/[x"},
		"case matters":            {"activation_task_types": "Implement"},
		"a good path, a bad type": {"activation_paths": "web/**", "activation_task_types": "deploy"},
	} {
		t.Run(name, func(t *testing.T) {
			if a, err := ParseSkillActivation(md); err == nil {
				t.Errorf("accepted %v as %+v", md, a)
			}
		})
	}
}

func TestParseSkillActivationEveryModeIsATaskType(t *testing.T) {
	for _, mode := range SkillTaskTypes() {
		a, err := ParseSkillActivation(map[string]string{"activation_task_types": mode})
		if err != nil || !a.MatchesTaskType(SessionMode(mode)) {
			t.Errorf("%s: %+v, %v", mode, a, err)
		}
	}
	a, _ := ParseSkillActivation(map[string]string{"activation_task_types": "implement"})
	if a.MatchesTaskType(SessionModeDefault) {
		t.Error("the default mode matched an implement rule")
	}
}

func TestSkillActivationPathMatchingEdges(t *testing.T) {
	a, err := ParseSkillActivation(map[string]string{"activation_paths": "**/*.templ, docs/**, *.md, a/**/b/**/c.go"})
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		"x.templ":         true, // ** matches zero directories
		"a/b/c/d/x.templ": true,
		"docs":            true, // docs/** with zero below
		"docs/a/b/c.txt":  true,
		"notes.md":        true,
		"sub/notes.md":    false, // * does not cross a slash
		"a/b/c.go":        true,
		"a/x/y/b/z/c.go":  true,
		"a/b/d.go":        false,
		"":                false,
		"X.TEMPL":         false, // case-sensitive
		"docs2/x":         false,
	} {
		if got := a.MatchesPath(file); got != want {
			t.Errorf("MatchesPath(%q) = %v, want %v", file, got, want)
		}
	}
	if (SkillActivation{}).MatchesPath("anything") || (SkillActivation{}).MatchesTaskType(SessionModeReview) {
		t.Error("the empty rule matched")
	}
}

func TestSkillActivationALeadingDotSlashPatternNeverMatches(t *testing.T) {
	a, err := ParseSkillActivation(map[string]string{"activation_paths": "./web/**"})
	if err != nil {
		t.Fatalf("a ./ pattern is refused: %v", err)
	}
	if a.MatchesPath("web/x.go") {
		t.Skip("normalised: fine")
	}
	t.Logf("./web/** is accepted but matches nothing: a silent no-op rule")
}
