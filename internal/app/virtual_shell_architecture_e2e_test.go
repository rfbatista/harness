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

// A repository's instruction files are read-only files of the shell, under
// project/architecture/<repository>/instructions/ (ADR 0057).

func (e *shellE2E) saveInstruction(repo, path, kind string, base int, content string) {
	e.t.Helper()
	e.post("save_instruction_file", map[string]any{"repository_id": repo, "path": path, "kind": kind, "base_version": base, "content": content}, nil)
}

func TestInstructionFilesAreReadOnlyFilesInEveryShellOfTheProject(t *testing.T) {
	e := newShellE2E(t)
	a := e.start(e.task("Instructions in the shell"))
	root := "project/architecture/harness/instructions/"
	if _, err := os.Stat(filepath.Join(a.shell, "project", "architecture", "harness")); err == nil {
		t.Fatal("a repository with no instruction files has a folder")
	}

	e.saveInstruction(e.repo, "CLAUDE.md", "memory", 0, "root memory")
	e.saveInstruction(e.repo, "web/AGENTS.md", "memory", 0, "web memory")
	e.saveInstruction(e.repo, ".claude/skills/review/SKILL.md", "skill", 0, "skill body")
	e.saveInstruction(e.repo, ".claude/agents/reviewer.md", "subagent", 0, "agent body")
	want := map[string]string{
		"CLAUDE.md": "root memory", "web/AGENTS.md": "web memory",
		".claude/skills/review/SKILL.md": "skill body", ".claude/agents/reviewer.md": "agent body",
	}
	eventually(t, "the files in the shell", func() error {
		for p := range want {
			if indexRow(t, a.shell, root+p) == nil {
				return os.ErrNotExist
			}
		}
		return nil
	})
	for p, content := range want {
		got, err := os.ReadFile(filepath.Join(a.shell, root+p))
		if err != nil || string(got) != content {
			t.Errorf("%s = %q (%v), want %q", p, got, err, content)
		}
		row := indexRow(t, a.shell, root+p)
		if row["kind"] != "instruction" || row["scope"] != "repository" || row["repository"] != "harness" || row["version"] != "1" {
			t.Errorf("index row of %s = %v", p, row)
		}
		if info, _ := os.Stat(filepath.Join(a.shell, root+p)); info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s is writable: %v", p, info.Mode())
		}
	}
	if readme, _ := os.ReadFile(filepath.Join(a.shell, "README.md")); !strings.Contains(string(readme), "instructions/") {
		t.Errorf("the README does not name the instructions folder:\n%s", readme)
	}

	// A shell opened later has them too, and every one follows a new version.
	b := e.start(e.task("Another task"))
	if indexRow(t, b.shell, root+"CLAUDE.md") == nil {
		t.Error("a shell opened later has no file")
	}
	e.saveInstruction(e.repo, "CLAUDE.md", "memory", 1, "root memory v2")
	eventually(t, "the new version", func() error {
		if row := indexRow(t, a.shell, root+"CLAUDE.md"); row == nil || row["version"] != "2" {
			return os.ErrNotExist
		}
		return nil
	})
	if got, _ := os.ReadFile(filepath.Join(a.shell, root+"CLAUDE.md")); string(got) != "root memory v2" {
		t.Errorf("the content did not follow: %s", got)
	}

	// A delete takes the file out.
	e.post("delete_instruction_file", map[string]any{"repository_id": e.repo, "path": "web/AGENTS.md", "base_version": 1}, nil)
	eventually(t, "the file to go", func() error {
		if _, err := os.Stat(filepath.Join(a.shell, root+"web/AGENTS.md")); err == nil {
			return os.ErrExist
		}
		return nil
	})
}

func TestAWriteIntoTheInstructionsFolderIsRefusedAsReadOnly(t *testing.T) {
	e := newShellE2E(t)
	e.saveInstruction(e.repo, "CLAUDE.md", "memory", 0, "memory")
	a := e.start(e.task("Read only instructions"))
	dir := filepath.Join(a.shell, "project", "architecture", "harness", "instructions")
	file := filepath.Join(dir, "CLAUDE.md")
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
		"create": {"write", []string{filepath.Join(dir, "AGENTS.md")}},
		"rm":     {"rm", []string{file}},
		"mv out": {"mv", []string{file, filepath.Join(a.shell, "project", "docs", "x.md")}},
		"mkdir":  {"mkdir", []string{filepath.Join(dir, "sub")}},
	} {
		allow, got := check(c.op, c.paths...)
		if allow || !strings.HasPrefix(got, "READ_ONLY: ") {
			t.Errorf("%s: allow=%v reason=%q", name, allow, got)
		}
	}
	if out := runHook(t, a, e.srv.URL, "PreToolUse", editTools, editInput("Write", file)); !strings.Contains(out, "READ_ONLY") {
		t.Errorf("PreToolUse Write = %s", out)
	}
}
