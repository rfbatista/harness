package claudehome

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readConfig(t *testing.T, h Home) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.Path(), ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHomeIsCreatedAndSeededSoClaudeStartsWithoutPrompts(t *testing.T) {
	h := NewHome(filepath.Join(t.TempDir(), "data", "claude-home"))
	if err := h.Ensure(); err != nil {
		t.Fatal(err)
	}
	m := readConfig(t, h)
	if m["hasCompletedOnboarding"] != true {
		t.Errorf("onboarding not marked done: %v", m)
	}
	if th, _ := m["theme"].(string); th == "" {
		t.Errorf("no theme: %v", m)
	}
}

func TestHomeSeedingKeepsWhatClaudeAndTheTranscriptsHold(t *testing.T) {
	h := NewHome(t.TempDir())
	transcript := filepath.Join(h.Path(), "projects", "-w", "c1.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Path(), ".claude.json"), []byte(`{"theme":"light","numStartups":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := h.Ensure(); err != nil {
			t.Fatal(err)
		}
	}
	m := readConfig(t, h)
	if m["theme"] != "light" || m["numStartups"] != 7.0 || m["hasCompletedOnboarding"] != true {
		t.Errorf("config after reseed: %v", m)
	}
	if b, err := os.ReadFile(transcript); err != nil || string(b) != "{}\n" {
		t.Errorf("transcript: %q, %v", b, err)
	}
}

func TestHomeTrustsSessionWorktreesIdempotently(t *testing.T) {
	h := NewHome(t.TempDir())
	for range 2 {
		if err := h.TrustFolder("/data/worktrees"); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.TrustFolder("/other"); err != nil {
		t.Fatal(err)
	}
	projects, _ := readConfig(t, h)["projects"].(map[string]any)
	for _, p := range []string{"/data/worktrees", "/other"} {
		e, _ := projects[p].(map[string]any)
		if e["hasTrustDialogAccepted"] != true {
			t.Errorf("%s not trusted: %v", p, projects)
		}
	}
	if len(projects) != 2 {
		t.Errorf("projects = %v", projects)
	}
}
