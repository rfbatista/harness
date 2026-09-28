package claudehome

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
)

func code(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestTranscripts_CanResume(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(t.TempDir(), "my_repo.wt", "agent-x")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	// Written under the resolved path, as the CLI does on macOS.
	projDir := filepath.Join(home, "projects", nonAlnum.ReplaceAllString(real, "-"))
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "c1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := Transcripts{Dir: home}

	if err := tr.CanResume(work, "c1"); err != nil {
		t.Errorf("existing transcript: %v", err)
	}
	if got := code(tr.CanResume(work, "c2")); got != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("missing transcript = %q, want SESSION_TRANSCRIPT_MISSING", got)
	}
	if got := code(tr.CanResume(filepath.Join(work, "gone"), "c1")); got != "WORKSPACE_MISSING" {
		t.Errorf("missing worktree = %q, want WORKSPACE_MISSING", got)
	}
}

func TestTranscripts_EncodesLikeTheCLI(t *testing.T) {
	got := nonAlnum.ReplaceAllString("/Users/me/personal_projects/api.v2", "-")
	if want := "-Users-me-personal-projects-api-v2"; got != want {
		t.Errorf("encoded = %q, want %q", got, want)
	}
}
