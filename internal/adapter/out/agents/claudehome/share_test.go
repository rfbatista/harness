package claudehome

import (
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/ports"
)

// A session of a project with a login runs with the project Claude home as its
// config dir, so claude files its transcript there. History, the transcript
// reader and resume read the agent Claude home: the project home must not
// split the transcripts (ADR 0102).
func TestAProjectClaudeHomeSharesItsTranscriptsWithTheAgentHome(t *testing.T) {
	agent := NewHome(t.TempDir())
	project := NewHome(filepath.Join(t.TempDir(), "claude-homes", "p1"))
	if err := agent.Ensure(); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := project.EnsureSharing(agent); err != nil {
		t.Fatal(err)
	}

	// claude, run with the project home as its config dir, files a transcript.
	slug := nonAlnum.ReplaceAllString(work, "-")
	dir := filepath.Join(project.Path(), "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "0b0c8b5c-2e1d-4a54-9c61-5f1d6c1b7a10"
	line := `{"type":"user","timestamp":"2026-11-20T10:00:00Z","message":{"role":"user","content":"hello from a project login"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (Transcripts{Dir: agent.Path()}).CanResume(work, id); err != nil {
		t.Errorf("resume does not find the project session: %v", err)
	}
	page, err := NewTranscriptReader(agent.Path()).ReadTranscript(work, id, ports.TranscriptQuery{Limit: 10})
	if err != nil || len(page.Entries) == 0 {
		t.Errorf("the transcript reader does not list the project session: %v %+v", err, page)
	}
	// And the other way: a session started under the agent home resumes with
	// the project home as its config dir.
	other := t.TempDir()
	otherDir := filepath.Join(agent.Path(), "projects", nonAlnum.ReplaceAllString(other, "-"))
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, id+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Transcripts{Dir: project.Path()}).CanResume(other, id); err != nil {
		t.Errorf("a session of the agent home does not resume under the project home: %v", err)
	}
}

func TestSharingATranscriptsFolderIsRepeatableAndKeepsWhatTheProjectHomeAlreadyHolds(t *testing.T) {
	agent := NewHome(t.TempDir())
	project := NewHome(t.TempDir())
	// A projects/ folder claude made in the project home before sharing.
	old := filepath.Join(project.Path(), "projects", "-old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "a.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := project.EnsureSharing(agent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(agent.Path(), "projects", "-old", "a.jsonl")); err != nil {
		t.Errorf("the transcript the project home held was lost: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(project.Path(), "projects")); err != nil || got != filepath.Join(agent.Path(), "projects") {
		t.Errorf("projects link = %q, %v", got, err)
	}
}

func TestTheAgentHomesSettingsAndPromptHistoryAreSharedOnceItHasThem(t *testing.T) {
	agent := NewHome(t.TempDir())
	project := NewHome(t.TempDir())
	if err := project.EnsureSharing(agent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(project.Path(), "settings.json")); err == nil {
		t.Error("a settings.json was made up for an agent home without one")
	}
	// The agent home gains them later; the next start links them.
	for name, content := range map[string]string{"settings.json": `{"model":"x"}`, "history.jsonl": "{}\n"} {
		if err := os.WriteFile(filepath.Join(agent.Path(), name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := project.EnsureSharing(agent); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(project.Path(), "settings.json"))
	if err != nil || string(got) != `{"model":"x"}` {
		t.Errorf("settings through the project home = %q, %v", got, err)
	}
	// claude appends to the history through the link.
	f, err := os.OpenFile(filepath.Join(project.Path(), "history.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{\"a\":1}\n")
	f.Close()
	if b, _ := os.ReadFile(filepath.Join(agent.Path(), "history.jsonl")); string(b) != "{}\n{\"a\":1}\n" {
		t.Errorf("the agent home's history = %q", b)
	}
}
