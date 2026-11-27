package bench

import "testing"

func baseSetup() Setup {
	return Setup{
		AgentName:    "go-developer",
		AgentPrompt:  "You write Go.",
		Skills:       []SetupFile{{Path: "skills/tdd/SKILL.md", Content: "red green"}},
		Instructions: []SetupFile{{Path: "CLAUDE.md", Content: "be brief"}, {Path: "ARCHITECTURE.md", Content: "layers"}},
		Model:        "sonnet",
		JudgeModel:   "opus",
		CLIVersion:   "2.1.0",
	}
}

func TestFingerprintIsEqualAcrossTwoBuildsOfAnUnchangedSetup(t *testing.T) {
	a, b := baseSetup(), baseSetup()
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("an unchanged setup changed its fingerprint")
	}
	// The order files were listed in is not a change.
	b.Instructions[0], b.Instructions[1] = b.Instructions[1], b.Instructions[0]
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("file order changed the fingerprint")
	}
}

func TestFingerprintChangesWhenAnyPartOfTheSetupChanges(t *testing.T) {
	changes := map[string]func(*Setup){
		"agent prompt":     func(s *Setup) { s.AgentPrompt = "You write Rust." },
		"skill":            func(s *Setup) { s.Skills[0].Content = "green red" },
		"instruction file": func(s *Setup) { s.Instructions[0].Content = "be verbose" },
		"model":            func(s *Setup) { s.Model = "opus" },
		"judge model":      func(s *Setup) { s.JudgeModel = "haiku" },
		"cli version":      func(s *Setup) { s.CLIVersion = "2.2.0" },
	}
	base := Fingerprint(baseSetup())
	for name, change := range changes {
		s := baseSetup()
		change(&s)
		if Fingerprint(s) == base {
			t.Errorf("a changed %s kept the fingerprint", name)
		}
	}
}
