//go:build !windows

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
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
