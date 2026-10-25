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

// The repository architecture in the virtual shell: one read-only YAML file
// per repository of the project that has a record (contract "Harness server ↔
// Agent sessions — repository architecture record").

func (e *shellE2E) saveArchitecture(repo string, base int, change func(map[string]any)) int {
	e.t.Helper()
	r := clone(sample())
	r["layers"] = []any{map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"}}}
	r["rules"] = []any{map[string]any{"id": "stay-in-your-layer", "kind": "edit_outside_layer", "mode": "warning", "message": "Ask the architect."}}
	if change != nil {
		change(r)
	}
	var out struct {
		Version int `json:"version"`
	}
	e.post("save_repository_architecture", map[string]any{"repository_id": repo, "base_version": base, "record": r}, &out)
	return out.Version
}

func (e *shellE2E) agents(names ...string) {
	e.t.Helper()
	for _, a := range names {
		if _, err := e.cat.Agents.CreateAgent(e.ctx, a, "", "", nil, nil); err != nil {
			e.t.Fatal(err)
		}
	}
}

func indexRow(t *testing.T, shell, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(shell, ".index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r["path"] == path {
			return r
		}
	}
	return nil
}

func TestTheRecordIsAReadOnlyYamlFileInEveryShellOfTheProject(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	a := e.start(e.task("Architecture in the shell"))
	file := "project/architecture/harness.yaml"
	if _, err := os.Stat(filepath.Join(a.shell, "project", "architecture")); err != nil {
		t.Fatalf("the folder is missing with no record: %v", err)
	}
	if row := indexRow(t, a.shell, file); row != nil {
		t.Fatalf("a repository with no record has a file: %v", row)
	}

	v := e.saveArchitecture(e.repo, 0, nil)
	eventually(t, "the record in the shell", func() error {
		if row := indexRow(t, a.shell, file); row == nil {
			return os.ErrNotExist
		}
		return nil
	})
	row := indexRow(t, a.shell, file)
	if row["kind"] != "architecture" || row["scope"] != "repository" || row["repository"] != "harness" || row["version"] != "1" || v != 1 {
		t.Errorf("index row = %v", row)
	}

	// Byte-identical to what an export writes.
	repo, err := e.cat.Projects.GetRepository(e.ctx, e.repo)
	if err != nil {
		t.Fatal(err)
	}
	e.post("export_repository_architecture", map[string]any{"repository_id": e.repo, "version": 1}, nil)
	exported, err := os.ReadFile(filepath.Join(repo.RootDir, ".harness", "architecture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	inShell, err := os.ReadFile(filepath.Join(a.shell, file))
	if err != nil || string(inShell) != string(exported) {
		t.Fatalf("the shell's file is not the export:\n%s\n--- vs ---\n%s (%v)", inShell, exported, err)
	}
	if info, _ := os.Stat(filepath.Join(a.shell, file)); info.Mode().Perm()&0o222 != 0 {
		t.Errorf("the file is writable: %v", info.Mode())
	}

	// A second shell of the project has it too, and follows a re-save.
	b := e.start(e.task("Another task"))
	if row := indexRow(t, b.shell, file); row == nil {
		t.Error("a shell opened later has no file")
	}
	e.saveArchitecture(e.repo, 1, func(r map[string]any) { r["pattern"] = "layered" })
	eventually(t, "the new version", func() error {
		if row := indexRow(t, a.shell, file); row == nil || row["version"] != "2" {
			return os.ErrNotExist
		}
		return nil
	})
	if text, _ := os.ReadFile(filepath.Join(a.shell, file)); !strings.Contains(string(text), "pattern: layered") {
		t.Errorf("the content did not follow: %s", text)
	}

	e.post("delete_repository_architecture", map[string]any{"repository_id": e.repo}, nil)
	eventually(t, "the file to go", func() error {
		if _, err := os.Stat(filepath.Join(a.shell, file)); err == nil {
			return os.ErrExist
		}
		return nil
	})
}

func TestAWriteIntoTheArchitectureFolderIsRefusedAsReadOnlyAndNamesTheWebUI(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	e.saveArchitecture(e.repo, 0, nil)
	a := e.start(e.task("Read only"))
	file := filepath.Join(a.shell, "project", "architecture", "harness.yaml")
	eventually(t, "the file", func() error {
		_, err := os.Stat(file)
		return err
	})
	const reason = "READ_ONLY: project/architecture/"
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
		"create": {"write", []string{filepath.Join(a.shell, "project", "architecture", "new.yaml")}},
		"rm":     {"rm", []string{file}},
		"mv out": {"mv", []string{file, filepath.Join(a.shell, "project", "docs", "x.yaml")}},
		"mv in":  {"mv", []string{filepath.Join(a.shell, "project", "docs", "x.md"), file}},
		"mkdir":  {"mkdir", []string{filepath.Join(a.shell, "project", "architecture", "sub")}},
		"rm dir": {"rm", []string{filepath.Join(a.shell, "project", "architecture")}},
	} {
		allow, got := check(c.op, c.paths...)
		if allow || !strings.HasPrefix(got, reason[:len("READ_ONLY: ")]) || !strings.Contains(got, "web UI") {
			t.Errorf("%s: allow=%v reason=%q", name, allow, got)
		}
	}
	// The hook says the same.
	if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, editInput("Write", file)); !strings.Contains(out, "READ_ONLY") || !strings.Contains(out, "web UI") {
		t.Errorf("PreToolUse Write = %s", out)
	}
}

func TestRepositoriesSharingANameGetDistinctFilesInTheShell(t *testing.T) {
	e := newShellE2E(t)
	e.agents("software-architect", "devops", "go-developer")
	second := filepath.Join(t.TempDir(), "harness")
	gitInit(t, second)
	r2, err := e.cat.Projects.CreateRepository(e.ctx, e.project, "harness", "", "https://example.com/other.git", second)
	if err != nil {
		t.Fatal(err)
	}
	e.saveArchitecture(e.repo, 0, nil)
	e.saveArchitecture(r2.ID, 0, nil)
	a := e.start(e.task("Same names"))
	if indexRow(t, a.shell, "project/architecture/harness.yaml") == nil {
		t.Error("the first repository has no plain name")
	}
	if indexRow(t, a.shell, "project/architecture/harness-"+r2.ID[:8]+".yaml") == nil {
		t.Errorf("the second repository has no short-id name")
	}
}
