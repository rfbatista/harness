//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// The repository prompts in the virtual shell: one read-only file per prompt
// that is set, at project/architecture/<repository>/agents/<agent>.md (ADR 0061).

func (e *shellE2E) saveRepositoryPrompt(agent, content, version string) {
	e.t.Helper()
	e.post("save_repository_prompt", map[string]any{"repository_id": e.repo, "agent": agent, "content": content, "version": version}, nil)
}

// promptVersion is the stored version of the agent's repository prompt.
func (e *shellE2E) promptVersion(agent string) string {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/get_repository_prompt?repository_id=" + e.repo + "&agent=" + agent)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		e.t.Fatal(err)
	}
	return out.Version
}

func TestRepositoryPromptsAreReadOnlyFilesInEveryShellOfTheProject(t *testing.T) {
	e := newShellE2E(t)
	e.agents("reviewer", "designer")
	a := e.start(e.task("Repository prompts in the shell"))
	root := "project/architecture/harness/agents/"
	file := filepath.Join(a.shell, root+"reviewer.md")

	e.saveRepositoryPrompt("reviewer", "Run make check before every commit.", "")
	eventually(t, "the file in a running shell", func() error {
		if indexRow(t, a.shell, root+"reviewer.md") == nil {
			return os.ErrNotExist
		}
		return nil
	})
	if got, err := os.ReadFile(file); err != nil || string(got) != "Run make check before every commit." {
		t.Fatalf("reviewer.md = %q (%v)", got, err)
	}
	row := indexRow(t, a.shell, root+"reviewer.md")
	if row["kind"] != "repository-prompt" || row["scope"] != "repository" || row["repository"] != "harness" {
		t.Errorf("index row = %v", row)
	}
	if info, _ := os.Stat(file); info.Mode().Perm()&0o222 != 0 {
		t.Errorf("reviewer.md is writable: %v", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(a.shell, root+"designer.md")); err == nil {
		t.Error("an agent with no prompt has a file")
	}

	// The file follows an edit, a shell opened later has it, and a blank save takes it out.
	e.saveRepositoryPrompt("reviewer", "Second text", e.promptVersion("reviewer"))
	eventually(t, "the new text", func() error {
		if got, _ := os.ReadFile(file); string(got) != "Second text" {
			return os.ErrNotExist
		}
		return nil
	})
	b := e.start(e.task("Another task"))
	if indexRow(t, b.shell, root+"reviewer.md") == nil {
		t.Error("a shell opened later has no file")
	}
	e.saveRepositoryPrompt("reviewer", "  ", e.promptVersion("reviewer"))
	eventually(t, "the file to go", func() error {
		if _, err := os.Stat(file); err == nil {
			return os.ErrExist
		}
		return nil
	})
}

func TestAWriteIntoAnAgentsRepositoryPromptIsRefusedAsReadOnlyAndNamesTheWebUI(t *testing.T) {
	e := newShellE2E(t)
	e.agents("reviewer")
	e.saveRepositoryPrompt("reviewer", "Run make check.", "")
	a := e.start(e.task("Read only repository prompts"))
	dir := filepath.Join(a.shell, "project", "architecture", "harness", "agents")
	file := filepath.Join(dir, "reviewer.md")
	eventually(t, "the file", func() error {
		_, err := os.Stat(file)
		return err
	})
	check := func(op string, paths ...string) (bool, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"op": op, "paths": paths})
		status, text := postJSON(t, e.srv.URL+"/api/sessions/"+a.id+"/shell/check", string(body))
		if status != http.StatusOK {
			t.Fatalf("%s = %d %s", op, status, text)
		}
		var ans ports.ShellAnswer
		if err := json.Unmarshal([]byte(text), &ans); err != nil {
			t.Fatal(err)
		}
		return ans.Allow, ans.Reason
	}
	for name, c := range map[string]struct {
		op    string
		paths []string
	}{
		"write":  {"write", []string{file}},
		"create": {"write", []string{filepath.Join(dir, "designer.md")}},
		"rm":     {"rm", []string{file}},
	} {
		allow, got := check(c.op, c.paths...)
		if allow || !strings.HasPrefix(got, "READ_ONLY: ") || !strings.Contains(got, "repository prompt") || !strings.Contains(got, "web UI") {
			t.Errorf("%s: allow=%v reason=%q", name, allow, got)
		}
	}
}
