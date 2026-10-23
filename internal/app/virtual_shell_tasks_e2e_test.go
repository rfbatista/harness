//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
)

// The other tasks of the project in a session's virtual shell:
// tasks/<id>/docs/ holds each one's task documents, with the semantics of
// task/docs/ (ADR 0016).

// A session's shell lists every other task of its project under
// tasks/<id>/docs/: that task's task-scope documents, named like task/docs,
// kept in step as documents and tasks change. Its own task's documents are
// in task/docs only.
func TestASessionsShellListsTheOtherTasksDocuments(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Feature"), e.task("Child")
	a := e.start(taskA)

	md := "# Brief\n\nwhat to build\n"
	brief := e.createDocument("Brief", md, "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskB, "document_id": brief.ID}, nil)
	eventually(t, "the child's brief in A", func() error {
		return hasFile(a.shell, "tasks/"+taskB+"/docs/brief.md", md, brief.ID, "task", domain.DocumentVersion(md))
	})
	if _, err := os.Stat(filepath.Join(a.shell, "task", "docs", "brief.md")); err == nil {
		t.Fatal("the child's brief is in A's task/docs")
	}

	// A's own document is in task/docs, and A is not listed under tasks/.
	own := e.createDocument("Own", "own\n", "text", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": own.ID}, nil)
	eventually(t, "A's own document", func() error { return hasFile(a.shell, "task/docs/own.txt", "own\n", own.ID, "task", "") })
	if _, err := os.Stat(filepath.Join(a.shell, "tasks", taskA)); err == nil {
		t.Fatal("the session's own task is listed under tasks/")
	}

	// A project document linked to the child is in project/docs only.
	terms := e.createDocument("Terms", "terms\n", "text", "project")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskB, "document_id": terms.ID}, nil)
	eventually(t, "the terms in project/docs", func() error { return hasFile(a.shell, "project/docs/terms.txt", "terms\n", terms.ID, "project", "") })
	if _, err := os.Stat(filepath.Join(a.shell, "tasks", taskB, "docs", "terms.txt")); err == nil {
		t.Fatal("a project document is listed under the child's docs")
	}

	// An update reaches the file; unlinked, it leaves the folder.
	v2 := "# Brief\n\nmore\n"
	e.post("update_document", map[string]string{"document_id": brief.ID, "title": "Brief", "content": v2}, nil)
	eventually(t, "the updated brief in A", func() error {
		return hasFile(a.shell, "tasks/"+taskB+"/docs/brief.md", v2, brief.ID, "task", domain.DocumentVersion(v2))
	})
	e.post("unlink_document_from_ticket", map[string]string{"ticket_id": taskB, "document_id": brief.ID}, nil)
	eventually(t, "the unlinked brief gone from A", func() error { return lacks(a.shell, brief.ID, "tasks/"+taskB+"/docs/brief.md") })

	// A new task's folder appears; a deleted task's goes.
	taskC := e.task("Later")
	eventually(t, "the new task's folder", func() error {
		info, err := os.Stat(filepath.Join(a.shell, "tasks", taskC, "docs"))
		if err == nil && !info.IsDir() {
			return fmt.Errorf("tasks/%s/docs is not a folder", taskC)
		}
		return err
	})
	e.post("delete_ticket", map[string]string{"ticket_id": taskC}, nil)
	eventually(t, "the deleted task's folder gone", func() error {
		if _, err := os.Stat(filepath.Join(a.shell, "tasks", taskC)); err == nil {
			return fmt.Errorf("tasks/%s is still there", taskC)
		}
		return nil
	})
}

// A file written under tasks/<id>/docs/ is a document on that task: created
// linked to it, titled from its first heading, task scope; written again,
// its content is stored and the session's base version moves on; the other
// task's own shell gets it in task/docs.
func TestAFileWrittenUnderAnotherTasksDocsIsThatTasksDocument(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Feature"), e.task("Child")
	a, b := e.start(taskA), e.start(taskB)

	md := "# Brief: child\n\nwhat to build\n"
	rel := "tasks/" + taskB + "/docs/brief.md"
	e.shellOp(a, "write", func() { writeShellFile(t, a, rel, md) }, rel)
	brief := indexedID(t, a, rel)
	wantDocument(t, e.document(brief), "Brief: child", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, md)
	switch {
	case !e.linkedTo(taskB, brief):
		t.Fatal("the brief is not linked to the child")
	case e.linkedTo(taskA, brief):
		t.Fatal("the brief is linked to the writer's task")
	}
	if err := hasFile(a.shell, rel, md, brief, "task", domain.DocumentVersion(md)); err != nil {
		t.Fatalf("the writer's base version: %v", err)
	}
	eventually(t, "the brief in the child's shell", func() error {
		return hasFile(b.shell, "task/docs/brief-child.md", md, brief, "task", domain.DocumentVersion(md))
	})

	// Written again: the content is stored at the next version, the title kept.
	v2 := "# A different heading\n\nacceptance criteria\n"
	e.shellOp(a, "write", func() { writeShellFile(t, a, rel, v2) }, rel)
	wantDocument(t, e.document(brief), "Brief: child", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, v2)
	if err := hasFile(a.shell, rel, v2, brief, "task", domain.DocumentVersion(v2)); err != nil {
		t.Fatalf("the writer's base version after the update: %v", err)
	}
	eventually(t, "the updated brief in the child's shell", func() error {
		return hasFile(b.shell, "task/docs/brief-child.md", v2, brief, "task", domain.DocumentVersion(v2))
	})

	// The writer's shell keeps the name it chose, with no second copy.
	idx, _ := shellIndex(a.shell)
	for p, row := range idx {
		if row.ID == brief && p != rel {
			t.Fatalf("a second copy at %s", p)
		}
	}

	// Removed, the document is deleted and leaves the child's shell.
	e.shellOp(a, "rm", func() { removeShellFile(t, a, rel) }, rel)
	if d := e.document(brief); d != nil {
		t.Fatalf("the removed document is still stored: %+v", d)
	}
	eventually(t, "the brief gone from the child's shell", func() error { return lacks(b.shell, brief, "task/docs/brief-child.md") })
}

// otherProjectTask is a task of a project that is not e.project.
func (e *shellE2E) otherProjectTask(title string) string {
	e.t.Helper()
	p, err := e.cat.Projects.CreateProject(e.ctx, "elsewhere", e.t.TempDir())
	if err != nil {
		e.t.Fatal(err)
	}
	tk, err := e.plan.CreateTicket(e.ctx, p.ID, title, "", domain.TicketStatusInProgress, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return tk.ID
}

// Under tasks/, only a document folder of a task of the session's project
// is written: a task that does not exist is TASK_NOT_FOUND, one of another
// project TASK_NOT_ON_PROJECT, the session's own task is at task/docs, and
// a file beside docs is NOT_STORED. A document moves between a task's docs
// and project/docs by the rules of task/docs, and never to another task's.
func TestWritesUnderTasksAreRefusedOutsideTheProjectsTasks(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB, taskC := e.task("Feature"), e.task("Child"), e.task("Other child")
	elsewhere := e.otherProjectTask("Elsewhere")
	a := e.start(taskA)

	refusals := []struct {
		op   string
		rel  string
		want shellAnswer
	}{
		{"write", "tasks/nope/docs/brief.md", shellAnswer{Code: "TASK_NOT_FOUND", Reason: "TASK_NOT_FOUND: tasks/nope/docs/brief.md: there is no task nope in this project; nothing was written"}},
		{"mkdir", "tasks/nope/docs", shellAnswer{Code: "TASK_NOT_FOUND", Reason: "TASK_NOT_FOUND: tasks/nope/docs: there is no task nope in this project; nothing was written"}},
		{"write", "tasks/" + elsewhere + "/docs/brief.md", shellAnswer{Code: "TASK_NOT_ON_PROJECT", Reason: "TASK_NOT_ON_PROJECT: tasks/" + elsewhere + "/docs/brief.md: task " + elsewhere + " is in another project; nothing was written"}},
		{"write", "tasks/" + taskA + "/docs/brief.md", shellAnswer{Code: "NOT_STORED", Reason: "NOT_STORED: tasks/" + taskA + "/docs/brief.md is this session's own task: its documents are in task/docs; nothing was written"}},
		{"write", "tasks/" + taskB + "/brief.md", shellAnswer{Code: "NOT_STORED", Reason: "NOT_STORED: tasks/" + taskB + "/brief.md is outside the folders the harness stores (task/docs, project/docs, tasks/<id>/docs, task/comments, task/designs, project/designs); nothing was written"}},
	}
	for _, r := range refusals {
		for _, step := range []string{"check", "commit"} {
			if got, _ := e.shellCall(a, step, r.op, r.rel); got != r.want {
				t.Errorf("%s %s %s = %+v, want %+v", step, r.op, r.rel, got, r.want)
			}
		}
	}
	// Written anyway, the commit is refused and the file taken away.
	writeShellFile(t, a, "tasks/nope/docs/brief.md", "# Brief\n")
	if got, _ := e.shellCall(a, "commit", "write", "tasks/nope/docs/brief.md"); got.Code != "TASK_NOT_FOUND" {
		t.Fatalf("commit a write on an unknown task = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(a.shell, "tasks", "nope", "docs", "brief.md")); err == nil {
		t.Fatal("the refused write stayed on disk")
	}

	// mkdir under a task's docs is allowed.
	e.shellOp(a, "mkdir", func() {
		if err := os.Mkdir(filepath.Join(a.shell, "tasks", taskB, "docs", "drafts"), 0o755); err != nil {
			t.Fatal(err)
		}
	}, "tasks/"+taskB+"/docs/drafts")

	// A document never moves to another task's docs, nor to project/docs.
	md := "# Brief\n\nwhat to build\n"
	rel := "tasks/" + taskB + "/docs/brief.md"
	e.shellOp(a, "write", func() { writeShellFile(t, a, rel, md) }, rel)
	brief := indexedID(t, a, rel)
	own := "# Own\n"
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/own.md", own) }, "task/docs/own.md")
	taskMove := func(src, dst string) shellAnswer {
		return shellAnswer{Code: "TASK_MOVE_DENIED", Reason: "TASK_MOVE_DENIED: " + src + " cannot move to " + dst + ": a document stays on its task; write it there instead; nothing was written"}
	}
	for _, mv := range [][2]string{
		{rel, "tasks/" + taskC + "/docs/brief.md"},
		{rel, "task/docs/brief.md"},
		{"task/docs/own.md", "tasks/" + taskB + "/docs/own.md"},
	} {
		if got, _ := e.shellCall(a, "check", "mv", mv[0], mv[1]); got != taskMove(mv[0], mv[1]) {
			t.Errorf("check mv %s %s = %+v", mv[0], mv[1], got)
		}
	}
	moveShellFile(t, a, rel, "tasks/"+taskC+"/docs/brief.md")
	if got, _ := e.shellCall(a, "commit", "mv", rel, "tasks/"+taskC+"/docs/brief.md"); got != taskMove(rel, "tasks/"+taskC+"/docs/brief.md") {
		t.Fatalf("commit mv to another task = %+v", got)
	}
	if err := hasFile(a.shell, rel, md, brief, "task", ""); err != nil {
		t.Fatalf("the refused move did not put the brief back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.shell, "tasks", taskC, "docs", "brief.md")); err == nil {
		t.Fatal("the refused move left the brief on the other task")
	}
	if !e.linkedTo(taskB, brief) || e.linkedTo(taskC, brief) {
		t.Fatal("the refused move changed the brief's task")
	}
	if got, _ := e.shellCall(a, "check", "mv", rel, "project/docs/brief.md"); got != projectMoveDenied(rel) {
		t.Fatalf("check mv to project/docs = %+v", got)
	}

	// From project/docs to a task's docs: the document becomes that task's
	// when linked to it, and is NOT_ON_TASK otherwise.
	terms := e.createDocument("Terms", "terms\n", "text", "project")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskB, "document_id": terms.ID}, nil)
	eventually(t, "the terms in A", func() error { return hasFile(a.shell, "project/docs/terms.txt", "terms\n", terms.ID, "project", "") })
	notOnTask := shellAnswer{Code: "NOT_ON_TASK", Reason: "NOT_ON_TASK: project/docs/terms.txt cannot move to task scope: the document is not linked to task " + taskC}
	if got, _ := e.shellCall(a, "check", "mv", "project/docs/terms.txt", "tasks/"+taskC+"/docs/terms.txt"); got != notOnTask {
		t.Fatalf("check mv to an unlinked task = %+v, want %+v", got, notOnTask)
	}
	to := "tasks/" + taskB + "/docs/terms.txt"
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "project/docs/terms.txt", to) }, "project/docs/terms.txt", to)
	wantDocument(t, e.document(terms.ID), "Terms", domain.DocumentFormatText, domain.DocumentScopeTask, "terms\n")
	if err := hasFile(a.shell, to, "terms\n", terms.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
}
