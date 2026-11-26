//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The task memory of a task and repository, read and edited through the API
// the task page uses (ADR 0013).

type memoryFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type memoryBody struct {
	TaskID       string       `json:"task_id"`
	RepositoryID string       `json:"repository_id"`
	Files        []memoryFile `json:"files"`
}

func (b memoryBody) file(name string) string {
	for _, f := range b.Files {
		if f.Name == name {
			return f.Content
		}
	}
	return "<missing " + name + ">"
}

func (e *shellE2E) taskMemory(task, repo string) (int, memoryBody, string) {
	e.t.Helper()
	b, _ := json.Marshal(map[string]string{"task_id": task, "repository_id": repo})
	status, text := postJSON(e.t, e.srv.URL+"/api/get_task_memory", string(b))
	var out memoryBody
	_ = json.Unmarshal([]byte(text), &out)
	return status, out, text
}

func (e *shellE2E) saveTaskMemory(task, repo, file, content string) (int, string) {
	e.t.Helper()
	b, _ := json.Marshal(map[string]string{"task_id": task, "repository_id": repo, "file": file, "content": content})
	return postJSON(e.t, e.srv.URL+"/api/save_task_memory", string(b))
}

func TestTaskMemory_ATaskWithNoMemoryYetAnswersTheFixedSections(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Memory")

	status, got, text := e.taskMemory(task, e.repo)
	if status != 200 || len(got.Files) != 2 {
		t.Fatalf("get = %d %s", status, text)
	}
	if !strings.Contains(got.file("plan.md"), "## Steps") {
		t.Errorf("plan.md = %q", got.file("plan.md"))
	}
	for _, section := range domain.ProgressSectionNames {
		if !strings.Contains(got.file("progress.md"), "## "+section) {
			t.Errorf("progress.md has no %q section: %q", section, got.file("progress.md"))
		}
	}
}

func TestTaskMemory_AnEditIsStoredReadBackAndOnTheFeed(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	task := e.task("Memory")
	other := e.task("Another task")

	if status, text := e.saveTaskMemory(task, e.repo, "progress.md", "# Progress\n\n## Done\n\n- the first step\n"); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	_, got, _ := e.taskMemory(task, e.repo)
	if !strings.Contains(got.file("progress.md"), "- the first step") {
		t.Errorf("read back = %q", got.file("progress.md"))
	}
	if !strings.Contains(got.file("plan.md"), "## Steps") {
		t.Errorf("plan.md lost its starter: %q", got.file("plan.md"))
	}
	feed.sawOnFeed("task_memory_changed", `"task_id":"`+task+`"`, `"repository_id":"`+e.repo+`"`, `"file":"progress.md"`)

	// Another task's memory is its own.
	_, theirs, _ := e.taskMemory(other, e.repo)
	if strings.Contains(theirs.file("progress.md"), "the first step") {
		t.Errorf("another task sees %q", theirs.file("progress.md"))
	}
}

func TestTaskMemory_RefusalsAreNotFoundOrInvalid(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Memory")
	if status, _, text := e.taskMemory("nope", e.repo); status != 404 || !strings.Contains(text, "TICKET_NOT_FOUND") {
		t.Errorf("unknown task = %d %s", status, text)
	}
	if status, _, text := e.taskMemory(task, "nope"); status != 404 || !strings.Contains(text, "REPOSITORY_NOT_FOUND") {
		t.Errorf("unknown repository = %d %s", status, text)
	}
	if status, text := e.saveTaskMemory(task, e.repo, "notes.md", "x"); status != 400 || !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("a file that is not plan.md or progress.md = %d %s", status, text)
	}
}

// Delivered to the sessions of a task as writable files outside the worktree,
// read back at each turn end (ADR 0013).

func (e *shellE2E) startRaw(task string) (id string, spec ports.AgentSpec) {
	e.t.Helper()
	sess, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: task, Mode: "implement"})
	if err != nil {
		e.t.Fatal(err)
	}
	return sess.ID, spec
}

func memoryDirOf(t *testing.T, spec ports.AgentSpec) string {
	t.Helper()
	for _, kv := range spec.Env {
		if v, ok := strings.CutPrefix(kv, ports.TaskMemoryDirEnv+"="); ok {
			return v
		}
	}
	t.Fatalf("no %s in the session's environment %v", ports.TaskMemoryDirEnv, spec.Env)
	return ""
}

func TestTaskMemory_ASecondSessionOfTheTaskSeesWhatTheFirstWroteAtItsTurnEnd(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	task := e.task("Memory delivery")

	first, spec := e.startRaw(task)
	dir := memoryDirOf(t, spec)
	if strings.HasPrefix(dir, spec.Dir) || !strings.HasPrefix(dir, e.dataDir) {
		t.Fatalf("the memory folder %s is not outside the worktree %s and inside the data dir %s", dir, spec.Dir, e.dataDir)
	}
	starter, err := os.ReadFile(filepath.Join(dir, "progress.md"))
	if err != nil || !strings.Contains(string(starter), "## Next step") {
		t.Fatalf("progress.md at start = %q, %v", starter, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plan.md")); err != nil {
		t.Fatal(err)
	}
	// The files are writable by the agent.
	wrote := "# Progress\n\n## Done\n\n- the parser\n\n## Failed and why\n\n## Next step\n\nthe hook\n\n## Open questions\n"
	if err := os.WriteFile(filepath.Join(dir, "progress.md"), []byte(wrote), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.orch.TurnEnded(e.ctx, first, false); err != nil {
		t.Fatal(err)
	}
	_, got, _ := e.taskMemory(task, e.repo)
	if got.file("progress.md") != wrote {
		t.Fatalf("the store after the turn end = %q", got.file("progress.md"))
	}
	feed.sawOnFeed("task_memory_changed", `"task_id":"`+task+`"`, `"file":"progress.md"`)

	second, spec2 := e.startRaw(task)
	if got, err := os.ReadFile(filepath.Join(memoryDirOf(t, spec2), "progress.md")); err != nil || string(got) != wrote {
		t.Fatalf("the second session's progress.md = %q, %v", got, err)
	}
	_ = second
	// The folder is the task's and repository's own.
	other := e.task("Another task")
	_, spec3 := e.startRaw(other)
	if memoryDirOf(t, spec3) == dir {
		t.Errorf("two tasks share the folder %s", dir)
	}
	// plan.md was not touched: no change was announced for it.
	if _, got, _ := e.taskMemory(task, e.repo); strings.Contains(got.file("plan.md"), "the parser") {
		t.Errorf("plan.md = %q", got.file("plan.md"))
	}
}

func TestTaskMemory_AFileTheSessionDidNotChangeNeverOverwritesAnotherSessionsWrite(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Two writers")
	a, specA := e.startRaw(task)
	b, specB := e.startRaw(task)
	dir := memoryDirOf(t, specA)
	if dir != memoryDirOf(t, specB) {
		t.Fatalf("the two sessions have different folders")
	}
	// The person edits the store while both run; neither session touched
	// the file, so neither turn end undoes the edit.
	if status, text := e.saveTaskMemory(task, e.repo, "plan.md", "# Plan\n\n## Steps\n\n- [doing] by the person\n"); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	for _, id := range []string{a, b} {
		if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, got, _ := e.taskMemory(task, e.repo); !strings.Contains(got.file("plan.md"), "by the person") {
		t.Fatalf("a turn end undid the person's edit: %q", got.file("plan.md"))
	}
	// And the running session sees the person's edit on disk after its turn end.
	if raw, _ := os.ReadFile(filepath.Join(dir, "plan.md")); !strings.Contains(string(raw), "by the person") {
		t.Errorf("plan.md on disk = %q", raw)
	}
}

func TestTaskMemory_TheStartingContextTrailRecordsTheMemory(t *testing.T) {
	e := newShellE2E(t)
	id, _ := e.startRaw(e.task("Trail"))
	view, err := e.orch.SessionContext(e.ctx, id)
	if err != nil || len(view.Entries) == 0 {
		t.Fatalf("context = %+v, %v", view, err)
	}
	var found bool
	for _, p := range view.Entries[0].Instructions {
		if p.Kind == domain.PieceMemory && p.Title == "Task memory" {
			found = true
		}
	}
	if !found {
		t.Errorf("no Task memory piece in %+v", view.Entries[0].Instructions)
	}
}

// What the agent wrote and the harness had not read back yet survives a
// restart: the next sync still knows the disk copy changed.
func TestTaskMemory_AnUnsyncedDiskEditSurvivesARestart(t *testing.T) {
	e := newShellE2EWith(t, "data.db")
	task := e.task("Restart")
	_, spec := e.startRaw(task)
	dir := memoryDirOf(t, spec)
	wrote := "# Progress\n\n## Done\n\n- before the restart\n\n## Failed and why\n\n## Next step\n\n## Open questions\n"
	if err := os.WriteFile(filepath.Join(dir, "progress.md"), []byte(wrote), 0o644); err != nil {
		t.Fatal(err)
	}
	e.restart()
	_, spec2 := e.startRaw(task)
	if raw, _ := os.ReadFile(filepath.Join(memoryDirOf(t, spec2), "progress.md")); string(raw) != wrote {
		t.Errorf("progress.md on disk after the restart = %q", raw)
	}
	if _, got, _ := e.taskMemory(task, e.repo); got.file("progress.md") != wrote {
		t.Errorf("the store after the restart = %q", got.file("progress.md"))
	}
}
