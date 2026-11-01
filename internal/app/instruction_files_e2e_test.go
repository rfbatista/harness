//go:build !windows

package app

import (
	"net/http"
	"strings"
	"testing"
)

func (s *archRecordServer) saveFile(path, kind string, base int, content string) (int, map[string]any) {
	return s.post("save_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": path, "kind": kind, "base_version": base, "content": content})
}

func (s *archRecordServer) listFiles() []map[string]any {
	s.t.Helper()
	var out []map[string]any
	for _, f := range s.mustPost("list_instruction_files", map[string]any{"repository_id": s.repo.ID})["files"].([]any) {
		out = append(out, f.(map[string]any))
	}
	return out
}

func (s *archRecordServer) withAgents() {
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		s.agent(a)
	}
}

func TestInstructionFiles_EachSaveIsANewVersionAndListCarriesNoContent(t *testing.T) {
	s := startArchRecordServer(t)
	for i, text := range []string{"one", "two", "three"} {
		status, meta := s.saveFile("CLAUDE.md", "memory", i, text)
		if status != http.StatusOK || meta["version"] != float64(i+1) || meta["size"] != float64(len(text)) {
			t.Fatalf("save %d: %d %v", i, status, meta)
		}
		if _, has := meta["content"]; has {
			t.Errorf("save answered content: %v", meta)
		}
	}
	files := s.listFiles()
	if len(files) != 1 || files[0]["path"] != "CLAUDE.md" || files[0]["kind"] != "memory" || files[0]["version"] != float64(3) || files[0]["updated_at"] == nil {
		t.Fatalf("files = %v", files)
	}
	if _, has := files[0]["content"]; has {
		t.Errorf("list carries content: %v", files[0])
	}
	got := s.mustPost("get_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "CLAUDE.md"})
	if got["content"] != "three" || got["version"] != float64(3) {
		t.Errorf("get = %v", got)
	}
	s.sawOnFeed("instruction_file_changed", `"repository_id":"`+s.repo.ID+`"`, `"path":"CLAUDE.md"`, `"version":3`)
}

func TestInstructionFiles_StaleExistingAndMissingAreRefused(t *testing.T) {
	s := startArchRecordServer(t)
	s.saveFile("CLAUDE.md", "memory", 0, "a")
	s.saveFile("AGENTS.md", "memory", 0, "b")

	cases := []struct {
		name   string
		verb   string
		body   map[string]any
		status int
		code   string
	}{
		{"stale save", "save_instruction_file", map[string]any{"path": "CLAUDE.md", "kind": "memory", "base_version": 0, "content": "x"}, 409, "INSTRUCTION_EXISTS"},
		{"stale version", "save_instruction_file", map[string]any{"path": "CLAUDE.md", "kind": "memory", "base_version": 7, "content": "x"}, 409, "INSTRUCTION_CHANGED"},
		{"edit of a missing file", "save_instruction_file", map[string]any{"path": "GEMINI.md", "kind": "memory", "base_version": 2, "content": "x"}, 404, "INSTRUCTION_NOT_FOUND"},
		{"get missing", "get_instruction_file", map[string]any{"path": "GEMINI.md"}, 404, "INSTRUCTION_NOT_FOUND"},
		{"rename onto existing", "rename_instruction_file", map[string]any{"path": "CLAUDE.md", "new_path": "AGENTS.md", "base_version": 1}, 409, "INSTRUCTION_EXISTS"},
		{"rename stale", "rename_instruction_file", map[string]any{"path": "CLAUDE.md", "new_path": "web/CLAUDE.md", "base_version": 9}, 409, "INSTRUCTION_CHANGED"},
		{"rename missing", "rename_instruction_file", map[string]any{"path": "GEMINI.md", "new_path": "web/GEMINI.md", "base_version": 1}, 404, "INSTRUCTION_NOT_FOUND"},
		{"delete stale", "delete_instruction_file", map[string]any{"path": "CLAUDE.md", "base_version": 9}, 409, "INSTRUCTION_CHANGED"},
		{"delete missing", "delete_instruction_file", map[string]any{"path": "GEMINI.md", "base_version": 1}, 404, "INSTRUCTION_NOT_FOUND"},
		{"unknown repository", "list_instruction_files", map[string]any{"repository_id": "nope"}, 404, "REPOSITORY_NOT_FOUND"},
	}
	for _, c := range cases {
		if _, has := c.body["repository_id"]; !has {
			c.body["repository_id"] = s.repo.ID
		}
		status, body := s.post(c.verb, c.body)
		if status != c.status || body["code"] != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.name, status, body, c.status, c.code)
		}
	}
	if got := s.mustPost("get_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "CLAUDE.md"}); got["content"] != "a" || got["version"] != float64(1) {
		t.Errorf("a refusal changed the file: %v", got)
	}
	if len(s.listFiles()) != 2 {
		t.Errorf("a refusal changed the list: %v", s.listFiles())
	}
}

func TestInstructionFiles_InvalidPathsAndSizesAreRefusedWithFieldErrors(t *testing.T) {
	s := startArchRecordServer(t)
	bad := []struct{ path, kind string }{
		{"/CLAUDE.md", "memory"},
		{"../CLAUDE.md", "memory"},
		{"a/../CLAUDE.md", "memory"},
		{".git/CLAUDE.md", "memory"},
		{"README.md", "memory"},
		{"CLAUDE.md", "bogus"},
		{".claude/skills/review.md", "skill"},
		{".claude/skills/review/", "skill"},
		{"skills/review/SKILL.md", "skill"},
		{".claude/agents/dir/x.md", "subagent"},
		{".claude/agents/x.txt", "subagent"},
		{"CLAUDE.md", "skill"},
	}
	for _, b := range bad {
		status, body := s.saveFile(b.path, b.kind, 0, "x")
		if status != http.StatusBadRequest || body["code"] != "INSTRUCTION_INVALID" || len(errorPaths(body)) == 0 {
			t.Errorf("%s as %s: %d %v", b.path, b.kind, status, body)
		}
	}
	good := []struct{ path, kind string }{
		{"AGENTS.md", "memory"}, {"web/GEMINI.md", "memory"},
		{".claude/skills/review/SKILL.md", "skill"}, {".claude/skills/review/refs/notes.md", "skill"},
		{".claude/agents/reviewer.md", "subagent"},
	}
	for _, g := range good {
		if status, body := s.saveFile(g.path, g.kind, 0, "x"); status != http.StatusOK {
			t.Errorf("%s as %s: %d %v", g.path, g.kind, status, body)
		}
	}

	if status, body := s.saveFile("big/CLAUDE.md", "memory", 0, strings.Repeat("a", 256*1024+1)); status != http.StatusBadRequest || body["code"] != "INSTRUCTION_INVALID" || errorPaths(body)["/content"] == "" {
		t.Errorf("256 KB + 1: %d %v", status, body)
	}
	if status, body := s.saveFile("edge/CLAUDE.md", "memory", 0, strings.Repeat("a", 256*1024)); status != http.StatusOK {
		t.Errorf("256 KB: %d %v", status, body)
	}
}

func TestInstructionFiles_AllFilesOfARepositoryAreAtMost4MiB(t *testing.T) {
	s := startArchRecordServer(t)
	chunk := strings.Repeat("a", 256*1024)
	for i := 0; i < 16; i++ {
		if status, body := s.saveFile(strings.Repeat("d", 1)+string(rune('a'+i))+"/CLAUDE.md", "memory", 0, chunk); status != http.StatusOK {
			t.Fatalf("file %d: %d %v", i, status, body)
		}
	}
	if status, body := s.saveFile("over/CLAUDE.md", "memory", 0, "a"); status != http.StatusBadRequest || body["code"] != "INSTRUCTION_INVALID" {
		t.Errorf("over 4 MiB: %d %v", status, body)
	}
	// an edit that does not grow the total still fits
	if status, body := s.saveFile("da/CLAUDE.md", "memory", 1, "small"); status != http.StatusOK {
		t.Errorf("shrinking edit: %d %v", status, body)
	}
}

func TestInstructionFiles_RenameAndDeleteKeepTheRecordEntryInStep(t *testing.T) {
	s := startArchRecordServer(t)
	s.withAgents()
	rec := clone(sample())
	rec["instructions"] = []any{
		map[string]any{"path": "CLAUDE.md", "kind": "memory", "applies_to": []any{"go-developer"}, "loaded": "always", "note": "n"},
		map[string]any{"path": "AGENTS.md", "kind": "memory", "applies_to": []any{"all"}, "loaded": "always"},
	}
	if status, body := s.save(0, rec); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	s.saveFile("CLAUDE.md", "memory", 0, "a")
	s.saveFile("AGENTS.md", "memory", 0, "b")

	paths := func() string {
		var out []string
		for _, in := range s.get()["record"].(map[string]any)["instructions"].([]any) {
			m := in.(map[string]any)
			note, _ := m["note"].(string)
			out = append(out, m["path"].(string)+":"+note)
		}
		return strings.Join(out, ",")
	}
	if status, meta := s.post("rename_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "CLAUDE.md", "new_path": "web/CLAUDE.md", "base_version": 1}); status != http.StatusOK || meta["path"] != "web/CLAUDE.md" || meta["version"] != float64(1) {
		t.Fatalf("rename: %d %v", status, meta)
	}
	if got := paths(); got != "web/CLAUDE.md:n,AGENTS.md:" {
		t.Errorf("after rename: %s", got)
	}
	s.sawOnFeed("instruction_file_changed", `"path":"web/CLAUDE.md"`, `"version":1`)
	s.sawOnFeed("instruction_file_changed", `"path":"CLAUDE.md"`, `"version":0`)
	if got := s.mustPost("get_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "web/CLAUDE.md"}); got["content"] != "a" {
		t.Errorf("rename lost the content: %v", got)
	}

	if status, _ := s.post("delete_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "AGENTS.md", "base_version": 1}); status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
	if got := paths(); got != "web/CLAUDE.md:n" {
		t.Errorf("after delete: %s", got)
	}
	if files := s.listFiles(); len(files) != 1 || files[0]["path"] != "web/CLAUDE.md" {
		t.Errorf("files = %v", files)
	}
	s.sawOnFeed("instruction_file_changed", `"path":"AGENTS.md"`, `"version":0`)
}

func TestInstructionFiles_DeletingTheArchitectureDeletesTheFiles(t *testing.T) {
	s := startArchRecordServer(t)
	s.withAgents()
	if status, body := s.save(0, sample()); status != http.StatusOK {
		t.Fatalf("%d %v", status, body)
	}
	s.saveFile("CLAUDE.md", "memory", 0, "a")
	s.mustPost("delete_repository_architecture", map[string]any{"repository_id": s.repo.ID})
	if files := s.listFiles(); len(files) != 0 {
		t.Errorf("files = %v", files)
	}
}

// copies is the repository_copies of a list, by path.
func (s *archRecordServer) copies() map[string]map[string]any {
	s.t.Helper()
	out := map[string]map[string]any{}
	for _, c := range s.mustPost("list_instruction_files", map[string]any{"repository_id": s.repo.ID})["repository_copies"].([]any) {
		m := c.(map[string]any)
		out[m["path"].(string)] = m
	}
	return out
}

func (s *archRecordServer) importFiles(paths ...string) (int, map[string]any) {
	return s.post("import_instruction_files", map[string]any{"repository_id": s.repo.ID, "paths": paths})
}

var repositoryInstructionFiles = map[string]string{
	"CLAUDE.md":                      "root memory",
	"AGENTS.md":                      "agents memory",
	"web/GEMINI.md":                  "gemini memory",
	".claude/skills/review/SKILL.md": "skill",
	".claude/skills/review/notes.md": "skill notes",
	".claude/agents/reviewer.md":     "reviewer",
	".claude/agents/notes.txt":       "not an agent",
	"docs/CLAUDE.md.bak":             "not memory",
	"main.go":                        "package main\n",
}

func TestInstructionFiles_RepositoryCopiesSayWhichDifferFromTheHarness(t *testing.T) {
	s := startArchRecordServer(t)
	s.commitFiles(repositoryInstructionFiles)

	copies := s.copies()
	kinds := map[string]string{
		"CLAUDE.md": "memory", "AGENTS.md": "memory", "web/GEMINI.md": "memory",
		".claude/skills/review/SKILL.md": "skill", ".claude/skills/review/notes.md": "skill", ".claude/agents/reviewer.md": "subagent",
	}
	if len(copies) != len(kinds) {
		t.Errorf("copies = %v", copies)
	}
	for p, kind := range kinds {
		if c := copies[p]; c == nil || c["kind"] != kind || c["differs"] != true {
			t.Errorf("copy %s = %v, want kind %s differing", p, c, kind)
		}
	}

	// The harness copy equal to the repository's does not differ; an edit does.
	s.saveFile("CLAUDE.md", "memory", 0, "root memory")
	if got := s.copies()["CLAUDE.md"]["differs"]; got != false {
		t.Errorf("an equal copy differs = %v", got)
	}
	s.saveFile("CLAUDE.md", "memory", 1, "edited in the harness")
	if got := s.copies()["CLAUDE.md"]["differs"]; got != true {
		t.Errorf("an edited copy differs = %v", got)
	}
	// A change on the default branch shows at once, with nothing cached.
	s.saveFile("CLAUDE.md", "memory", 2, "root memory")
	s.commitFiles(map[string]string{"CLAUDE.md": "root memory, changed"})
	if got := s.copies()["CLAUDE.md"]["differs"]; got != true {
		t.Errorf("a changed repository copy differs = %v", got)
	}
}

func TestInstructionFiles_ImportCopiesFromTheDefaultBranchAndAddsVersions(t *testing.T) {
	s := startArchRecordServer(t)
	s.commitFiles(repositoryInstructionFiles)
	status, body := s.importFiles("CLAUDE.md", ".claude/agents/reviewer.md")
	if status != http.StatusOK {
		t.Fatalf("import: %d %v", status, body)
	}
	files := body["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["path"] != "CLAUDE.md" || files[0].(map[string]any)["kind"] != "memory" || files[0].(map[string]any)["version"] != float64(1) ||
		files[1].(map[string]any)["kind"] != "subagent" {
		t.Fatalf("files = %v", files)
	}
	if got := s.mustPost("get_instruction_file", map[string]any{"repository_id": s.repo.ID, "path": "CLAUDE.md"}); got["content"] != "root memory" {
		t.Errorf("content = %v", got)
	}
	s.sawOnFeed("instruction_file_changed", `"path":".claude/agents/reviewer.md"`, `"version":1`)
	if c := s.copies()["CLAUDE.md"]; c["differs"] != false {
		t.Errorf("after import = %v", c)
	}

	// A repository change makes version 2; an unchanged file stays as it is.
	s.commitFiles(map[string]string{"CLAUDE.md": "root memory v2"})
	status, body = s.importFiles("CLAUDE.md", ".claude/agents/reviewer.md")
	if status != http.StatusOK {
		t.Fatalf("second import: %d %v", status, body)
	}
	versions := map[string]any{}
	for _, f := range body["files"].([]any) {
		versions[f.(map[string]any)["path"].(string)] = f.(map[string]any)["version"]
	}
	if versions["CLAUDE.md"] != float64(2) || versions[".claude/agents/reviewer.md"] != float64(1) {
		t.Errorf("versions = %v", versions)
	}

	// An unknown path, or one that is no instruction file, imports nothing.
	for _, bad := range []string{"missing/CLAUDE.md", "main.go"} {
		status, body = s.importFiles("AGENTS.md", bad)
		if status < 400 || (body["code"] != "INSTRUCTION_NOT_FOUND" && body["code"] != "INSTRUCTION_INVALID") {
			t.Errorf("import with %s: %d %v", bad, status, body)
		}
	}
	for _, f := range s.listFiles() {
		if f["path"] == "AGENTS.md" {
			t.Error("a refused import stored a file")
		}
	}
	if status := gitE2E(t, s.checkout, "status", "--porcelain"); status != "" {
		t.Errorf("import changed the checkout: %s", status)
	}
}
