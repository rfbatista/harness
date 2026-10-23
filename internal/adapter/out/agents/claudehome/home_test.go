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

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestAFreshHomeIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude-home")
	if err := NewHome(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("home mode = %o, want 700", m)
	}
}

func TestALooserHomeIsTightenedAndItsLoginFileRepairedWithoutBeingRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude-home")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cred := filepath.Join(dir, ".credentials.json")
	secret := "sk-ant-oat01-do-not-touch"
	if err := os.WriteFile(cred, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cred, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewHome(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("home mode = %o, want 700", m)
	}
	if m := mode(t, cred); m != 0o600 {
		t.Errorf("login file mode = %o, want 600", m)
	}
	if b, _ := os.ReadFile(cred); string(b) != secret {
		t.Errorf("login file content changed: %q", b)
	}
}

func TestATighterLoginFileIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	cred := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(cred, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := NewHome(dir).Ensure(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, cred); m != 0o400 {
		t.Errorf("login file mode = %o, want 400 kept", m)
	}
}

func TestTheHomeHoldsALoginOnlyWhenItsCredentialsFileHasAClaudeAccountLogin(t *testing.T) {
	for name, tc := range map[string]struct {
		content *string
		want    bool
	}{
		"no file":      {nil, false},
		"login":        {ptr(`{"claudeAiOauth":{"accessToken":"a","refreshToken":"r"}}`), true},
		"other keys":   {ptr(`{"mcpOAuth":{},"claudeAiOauth":{"accessToken":"a"}}`), true},
		"empty":        {ptr(``), false},
		"not json":     {ptr(`claudeAiOauth`), false},
		"no login key": {ptr(`{"mcpOAuth":{}}`), false},
		"null login":   {ptr(`{"claudeAiOauth":null}`), false},
		"string login": {ptr(`{"claudeAiOauth":"x"}`), false},
	} {
		h := NewHome(t.TempDir())
		if tc.content != nil {
			if err := os.WriteFile(filepath.Join(h.Path(), ".credentials.json"), []byte(*tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.HasLogin(); got != tc.want {
			t.Errorf("%s: HasLogin = %v, want %v", name, got, tc.want)
		}
	}
}

func ptr(s string) *string { return &s }
