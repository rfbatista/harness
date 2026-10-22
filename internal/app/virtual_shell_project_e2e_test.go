//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A product session started on the project with no task (ADR 0016) gets the
// project's shell: no task/, every task of the project under tasks/<id>/docs/.
// It creates the feature task with create_task (an explicit empty parent),
// the task's folder appears, the PRD written there is a document on the
// feature task, and a write under task/ is NOT_STORED.
func TestAProjectLevelSessionWritesThePRDOntoTheFeatureTask(t *testing.T) {
	e := newShellE2E(t)
	existing := e.task("Existing")
	s := e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, Mode: "product"})

	if _, err := os.Stat(filepath.Join(s.shell, "task")); err == nil {
		t.Fatal("a shell with no task has a task/ folder")
	}
	if info, err := os.Stat(filepath.Join(s.shell, "tasks", existing, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("tasks/%s/docs: %v", existing, err)
	}
	idx, err := shellIndex(s.shell)
	if err != nil {
		t.Fatal(err)
	}
	for p := range idx {
		if strings.HasPrefix(p, "task/") {
			t.Errorf("the shell indexes %s", p)
		}
	}

	// task/ is not stored here, and the reason says where to write.
	want := shellAnswer{Code: "NOT_STORED", Reason: "NOT_STORED: task/docs/prd.md: this session has no task, so there is no task/ folder; write documents under tasks/<id>/docs (create_task first) or project/docs; nothing was written"}
	for _, step := range []string{"check", "commit"} {
		if got, _ := e.shellCall(s, step, "write", "task/docs/prd.md"); got != want {
			t.Errorf("%s write task/docs/prd.md = %+v, want %+v", step, got, want)
		}
	}

	// The tools that act on a session's task refuse; create_task works and
	// makes a top-level task.
	mcp := e.dial(s)
	if text, isErr := callTaskRaw(t, mcp, "get_task", nil); !isErr || !strings.Contains(text, "SESSION_HAS_NO_TASK") {
		t.Fatalf("get_task without a task = %q (error %v)", text, isErr)
	}
	var created struct {
		Task domain.Ticket `json:"task"`
	}
	if err := json.Unmarshal([]byte(callTask(t, mcp, "create_task", map[string]any{"title": "Export", "description": "People export their data.", "parent_id": ""})), &created); err != nil {
		t.Fatal(err)
	}
	feature := created.Task
	if feature.ID == "" || feature.ParentID != "" || feature.ProjectID != e.project {
		t.Fatalf("create_task made %+v, want a top-level task of the project", feature)
	}
	eventually(t, "the feature task's folder", func() error {
		info, err := os.Stat(filepath.Join(s.shell, "tasks", feature.ID, "docs"))
		if err == nil && !info.IsDir() {
			return fmt.Errorf("tasks/%s/docs is not a folder", feature.ID)
		}
		return err
	})

	// The PRD written under the feature task is its document.
	md := "# PRD: Export\n\n## Goal\n\nLeave with the data.\n"
	rel := "tasks/" + feature.ID + "/docs/prd.md"
	e.shellOp(s, "write", func() { writeShellFile(t, s, rel, md) }, rel)
	prd := indexedID(t, s, rel)
	wantDocument(t, e.document(prd), "PRD: Export", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, md)
	if !e.linkedTo(feature.ID, prd) {
		t.Fatal("the PRD is not linked to the feature task")
	}

	// A child under the feature, and its brief.
	if err := json.Unmarshal([]byte(callTask(t, mcp, "create_task", map[string]any{"title": "CSV writer", "description": "Write the file.", "parent_id": feature.ID})), &created); err != nil {
		t.Fatal(err)
	}
	child := created.Task
	if child.ParentID != feature.ID {
		t.Fatalf("child = %+v, want parent %s", child, feature.ID)
	}
	brief := "tasks/" + child.ID + "/docs/brief.md"
	eventually(t, "the child's folder", func() error {
		_, err := os.Stat(filepath.Join(s.shell, "tasks", child.ID, "docs"))
		return err
	})
	e.shellOp(s, "write", func() { writeShellFile(t, s, brief, "# Brief: CSV writer\n") }, brief)
	if !e.linkedTo(child.ID, indexedID(t, s, brief)) {
		t.Fatal("the brief is not linked to the child")
	}
}
