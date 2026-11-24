//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ownerSession starts a thread and dials its session's tools, as the claude
// the server launched would.
func (e *poE2E) ownerSession() (threadID string, c *client.Client) {
	e.t.Helper()
	id, _ := e.startThread("shape a feature")
	e.waitTurn(id, "idle")
	return id, e.dialSession(e.thread(id).Thread.SessionID)
}

// madeTask calls a task tool of the product owner and decodes the task it
// answers.
func (e *poE2E) madeTask(c *client.Client, tool string, args map[string]any) attributedRow {
	e.t.Helper()
	text, refused := e.call(c, tool, args)
	if refused {
		e.t.Fatalf("%s %v was refused: %s", tool, args, text)
	}
	var out struct {
		Task attributedRow `json:"task"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Task.ID == "" {
		e.t.Fatalf("%s answered %s", tool, text)
	}
	return out.Task
}

func TestTheProductOwnerCreatesAFeatureTaskAndItsChildAttributedToItsThread(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()

	feature := e.madeTask(owner, "create_task", map[string]any{"title": "Feature", "description": "What it is", "parent_id": ""})
	child := e.madeTask(owner, "create_task", map[string]any{"title": "Child", "parent_id": feature.ID})

	for _, id := range []string{feature.ID, child.ID} {
		row := e.taskRow(id)
		if row.CreatedBy == nil || row.CreatedBy.Kind != "product_owner" || row.CreatedBy.ThreadID != threadID || row.CreatedBy.At == "" {
			t.Errorf("task %s created_by = %+v, want the product owner with thread %s", id, row.CreatedBy, threadID)
		}
	}
	var got struct {
		Ticket struct {
			Status   string `json:"status"`
			ParentID string `json:"parent_id"`
		} `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+child.ID, &got)
	if got.Ticket.ParentID != feature.ID || got.Ticket.Status != "backlog" {
		t.Errorf("the child = %+v, want a backlog child of the feature", got.Ticket)
	}
	eventuallyPO(t, "the board's feed carries the product owner's tasks", func() string {
		n := 0
		for _, c := range e.feed.changesOf("ticket") {
			if strings.Contains(c, "product_owner") {
				n++
			}
		}
		if n < 2 {
			return "ticket events by the product owner on the feed: " + string(rune('0'+n))
		}
		return ""
	})
}

func TestTheProductOwnerChangesATasksTitleDescriptionAndStatusAndRecordsItself(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	made := e.madeTask(owner, "create_task", map[string]any{"title": "Draft", "parent_id": ""})

	e.madeTask(owner, "update_task", map[string]any{"task_id": made.ID, "title": "Final", "description": "Now described", "status": "review"})

	var got struct {
		Ticket struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"`
		} `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+made.ID, &got)
	if got.Ticket.Title != "Final" || got.Ticket.Description != "Now described" || got.Ticket.Status != "review" {
		t.Errorf("the task = %+v, want the changed title, description and status", got.Ticket)
	}
	if row := e.taskRow(made.ID); row.ChangedBy == nil || row.ChangedBy.Kind != "product_owner" || row.ChangedBy.ThreadID != threadID {
		t.Errorf("changed_by = %+v, want the product owner with thread %s", row.ChangedBy, threadID)
	}
}

func TestTheProductOwnerCannotMoveATaskToDoneNorChangeATaskOfAnotherProject(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerSession()
	made := e.madeTask(owner, "create_task", map[string]any{"title": "Work", "parent_id": ""})

	text, refused := e.call(owner, "update_task", map[string]any{"task_id": made.ID, "status": "done", "title": "Should not apply"})
	if !refused || !strings.Contains(text, "Only the person moves a task to done") {
		t.Fatalf("a move to done = %q refused=%v, want only the person moves a task to done", text, refused)
	}
	var got struct {
		Ticket struct{ Title, Status string } `json:"ticket"`
	}
	e.get("get_ticket?ticket_id="+made.ID, &got)
	if got.Ticket.Status != "backlog" || got.Ticket.Title != "Work" {
		t.Errorf("after a refused move the task = %+v, want it unchanged", got.Ticket)
	}
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.plan.CreateTicket(e.ctx, other.ID, "Not yours", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	if text, refused := e.call(owner, "update_task", map[string]any{"task_id": foreign.ID, "title": "Changed"}); !refused || !strings.Contains(text, "TASK_NOT_FOUND") {
		t.Errorf("a task of another project = %q refused=%v, want TASK_NOT_FOUND", text, refused)
	}
	if again, _ := e.plan.GetTicket(e.ctx, foreign.ID); again.Title != "Not yours" {
		t.Error("the product owner changed a task of another project")
	}
}

func TestTheProductOwnerIsServedTaskWriteToolsButNoDeleteNorStartAndNoOtherModeGetsItsTools(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerSession()

	got := e.toolNames(owner)
	for _, want := range []string{"create_task", "update_task"} {
		if !slices.Contains(got, want) {
			t.Errorf("the product owner is not served %s: %v", want, got)
		}
	}
	for _, name := range got {
		if strings.Contains(name, "delete") || strings.Contains(name, "start") || strings.Contains(name, "archive") {
			t.Errorf("the product owner is served %s", name)
		}
	}
	for _, banned := range []string{"start_task_session", "delete_ticket", "delete_document", "update_task_status", "archive_ticket"} {
		if text, refused := e.call(owner, banned, map[string]any{"task_id": "x", "prompt": "x", "status": "done"}); !refused {
			t.Errorf("a call to %s was answered: %s", banned, text)
		}
	}

	// Another mode's session keeps its own tools and none of the product owner's.
	if _, err := e.sess.Create(&domain.Session{ID: "product-session", ProjectID: e.project, Task: "a product session", Status: domain.SessionRunning, Mode: domain.SessionModeProduct}); err != nil {
		t.Fatal(err)
	}
	other := e.toolNames(e.dialSession("product-session"))
	if slices.Contains(other, "update_task") || slices.Contains(other, "get_project_task") {
		t.Errorf("a product session is served the product owner's tools: %v", other)
	}
	if !slices.Contains(other, "create_task") || !slices.Contains(other, "update_task_status") {
		t.Errorf("a product session lost its own tools: %v", other)
	}
}

func TestTheOpeningBriefNamesTheProductOwnersTaskWritesAndTheLinesItNeverCrosses(t *testing.T) {
	e := newPOE2E(t)
	e.ownerSession()
	l := e.launches()
	if len(l) != 1 {
		t.Fatalf("launches = %q", l)
	}
	for _, want := range []string{"update_task", "only the person moves a task to done", "never delete", "start a session"} {
		if !strings.Contains(strings.ToLower(l[0]), strings.ToLower(want)) {
			t.Errorf("the opening brief does not say %q", want)
		}
	}
}

// ownerShellWrite writes a file in the product owner's shell the way its Write
// tool does: the file lands on disk, then the harness is asked to commit it.
func (e *poE2E) ownerShellWrite(threadID, rel, content string) ports.ShellAnswer {
	e.t.Helper()
	dir, ok := e.shells.RootDir(e.thread(threadID).Thread.SessionID)
	if !ok {
		e.t.Fatal("the product owner has no virtual shell")
	}
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	edit := ports.ShellEdit{Op: "write", Paths: []string{path}, Tool: "Write"}
	if ans := e.shells.CheckShellEdit(e.ctx, e.thread(threadID).Thread.SessionID, edit); !ans.Allow {
		return ans
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return e.shells.CommitShellEdit(e.ctx, e.thread(threadID).Thread.SessionID, edit)
}

func (e *poE2E) documentByTitle(title string) *domain.Document {
	for _, d := range e.plan.ListDocuments(e.project, "") {
		if d.Title == title {
			return d
		}
	}
	return nil
}

func TestTheProductOwnerWritesAPrdOnAFeatureAndABriefOnAChildAndEditsAProjectDocument(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	feature := e.madeTask(owner, "create_task", map[string]any{"title": "Feature", "parent_id": ""})
	child := e.madeTask(owner, "create_task", map[string]any{"title": "Child", "parent_id": feature.ID})

	if ans := e.ownerShellWrite(threadID, "tasks/"+feature.ID+"/docs/prd.md", "# PRD: Feature\n\n## Goal\n"); !ans.Allow {
		t.Fatalf("the PRD write = %+v", ans)
	}
	if ans := e.ownerShellWrite(threadID, "tasks/"+child.ID+"/docs/brief.md", "# Brief: Child\n"); !ans.Allow {
		t.Fatalf("the brief write = %+v", ans)
	}
	if ans := e.ownerShellWrite(threadID, "project/docs/conventions.md", "# Conventions\n\nOne\n"); !ans.Allow {
		t.Fatalf("the project document write = %+v", ans)
	}

	prd := e.documentByTitle("PRD: Feature")
	if prd == nil || prd.Scope != domain.DocumentScopeTask || prd.CreatedBy == nil || prd.CreatedBy.Kind != domain.AuthorProductOwner || prd.CreatedBy.ThreadID != threadID {
		t.Fatalf("the PRD = %+v, want a task document made by the product owner's thread", prd)
	}
	if links := e.plan.ListTicketDocuments(e.ctx, feature.ID); len(links) != 1 || links[0].ID != prd.ID {
		t.Errorf("the feature's documents = %v, want its PRD", links)
	}
	if brief := e.documentByTitle("Brief: Child"); brief == nil || len(e.plan.ListTicketDocuments(e.ctx, child.ID)) != 1 {
		t.Errorf("the child's brief = %+v", brief)
	}
	conv := e.documentByTitle("Conventions")
	if conv == nil || conv.Scope != domain.DocumentScopeProject || conv.CreatedBy == nil || conv.CreatedBy.Kind != domain.AuthorProductOwner {
		t.Fatalf("the project document = %+v", conv)
	}

	// An edit of it is stored against the version the shell had, and is the product owner's.
	if ans := e.ownerShellWrite(threadID, "project/docs/conventions.md", "# Conventions\n\nTwo\n"); !ans.Allow {
		t.Fatalf("the edit = %+v", ans)
	}
	if edited := e.documentByTitle("Conventions"); edited == nil || !strings.Contains(edited.Content, "Two") || edited.ChangedBy == nil || edited.ChangedBy.Kind != domain.AuthorProductOwner || edited.ChangedBy.ThreadID != threadID {
		t.Fatalf("the edited document = %+v", edited)
	}
}

func TestTheProductOwnersShellRefusesEveryDeleteAndEveryWriteBeyondDocuments(t *testing.T) {
	e := newPOE2E(t)
	threadID, _ := e.ownerSession()
	sid := e.thread(threadID).Thread.SessionID
	if ans := e.ownerShellWrite(threadID, "project/docs/keep.md", "# Keep\n"); !ans.Allow {
		t.Fatalf("the write = %+v", ans)
	}
	dir, _ := e.shells.RootDir(sid)
	keep := filepath.Join(dir, "project/docs/keep.md")

	rm := ports.ShellEdit{Op: "rm", Paths: []string{keep}}
	if err := os.Remove(keep); err != nil {
		t.Fatal(err)
	}
	if ans := e.shells.CommitShellEdit(e.ctx, sid, rm); ans.Allow || ans.Code != "PRODUCT_OWNER_NO_DELETE" {
		t.Errorf("rm = %+v, want PRODUCT_OWNER_NO_DELETE", ans)
	}
	if ans := e.shells.CheckShellEdit(e.ctx, sid, rm); ans.Allow || ans.Code != "PRODUCT_OWNER_NO_DELETE" {
		t.Errorf("rm check = %+v, want PRODUCT_OWNER_NO_DELETE", ans)
	}
	if e.documentByTitle("Keep") == nil {
		t.Error("an rm removed the document")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("the refused rm left no file in the shell: %v", err)
	}

	for _, rel := range []string{"task/docs/x.md", "task/comments/c.md", "project/designs/d.html", "README.md", "elsewhere.md"} {
		if ans := e.ownerShellWrite(threadID, rel, "# X\n"); ans.Allow {
			t.Errorf("a write to %s = %+v, want it refused", rel, ans)
		}
	}
	if ans := e.shells.CheckShellEdit(e.ctx, sid, ports.ShellEdit{Op: "write", Paths: []string{"/tmp/elsewhere"}}); !ans.Allow {
		t.Errorf("a path outside the shell = %+v", ans)
	}
}
