//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
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
		if strings.Contains(name, "delete") || strings.Contains(name, "start") || (strings.Contains(name, "archive") && name != "archive_task") {
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

// poHeld is a held_action item of a thread as the person reads it.
type poHeld struct {
	ItemID       string `json:"item_id"`
	Kind         string `json:"kind"`
	HeldActionID string `json:"held_action_id"`
	Summary      string `json:"summary"`
	State        string `json:"state"`
	Target       struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"target"`
}

func (e *poE2E) heldActions(threadID string) []poHeld {
	e.t.Helper()
	var raw struct {
		Items []json.RawMessage `json:"items"`
	}
	e.get("product_owner_thread?thread_id="+threadID, &raw)
	var out []poHeld
	for _, r := range raw.Items {
		var h poHeld
		if json.Unmarshal(r, &h) == nil && h.Kind == "held_action" {
			if strings.Contains(string(r), `"held"`) {
				e.t.Errorf("a held action item carries what applies it on the wire: %s", r)
			}
			out = append(out, h)
		}
	}
	return out
}

func (e *poE2E) pendingHeld(threadID string) int {
	e.t.Helper()
	var list struct {
		Threads []struct {
			ThreadID           string `json:"thread_id"`
			PendingHeldActions int    `json:"pending_held_actions"`
		} `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &list)
	for _, th := range list.Threads {
		if th.ThreadID == threadID {
			return th.PendingHeldActions
		}
	}
	e.t.Fatalf("thread %s is not listed", threadID)
	return 0
}

// doneTask is a done task of the person's: only the person moves one there.
func (e *poE2E) doneTask(title string) *domain.Ticket {
	e.t.Helper()
	tk, err := e.plan.CreateTicket(e.ctx, e.project, title, "", domain.TicketStatusBacklog, "")
	if err != nil {
		e.t.Fatal(err)
	}
	done := domain.TicketStatusDone
	if tk, err = e.plan.PatchTicket(e.ctx, tk.ID, ports.TicketPatch{Status: &done}); err != nil {
		e.t.Fatal(err)
	}
	return tk
}

func TestAnArchiveByTheProductOwnerWaitsForTheConfirmAndHappensOnlyThen(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Finished")
	turns := len(e.launches())

	text, refused := e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	if refused || !strings.Contains(text, "Awaiting the person") {
		t.Fatalf("archive_task = %q refused=%v, want it awaiting the person", text, refused)
	}
	if again, _ := e.plan.GetTicket(e.ctx, tk.ID); again.ArchivedAt != nil {
		t.Fatal("the product owner archived the task itself")
	}
	held := e.heldActions(threadID)
	if len(held) != 1 || held[0].State != "pending" || held[0].Target.Kind != "task" || held[0].Target.ID != tk.ID ||
		held[0].Target.Title != "Finished" || held[0].Target.URL != "/projects/"+e.project+"/tasks/"+tk.ID || held[0].Summary == "" || held[0].HeldActionID == "" {
		t.Fatalf("the held action = %+v", held)
	}
	if n := e.pendingHeld(threadID); n != 1 {
		t.Fatalf("pending_held_actions = %d, want 1", n)
	}

	var answer struct {
		State   string `json:"state"`
		Outcome struct {
			Task attributedRow `json:"task"`
		} `json:"outcome"`
	}
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": held[0].HeldActionID, "answer": "confirm"}, &answer); status != 200 {
		t.Fatalf("confirm = %d %s", status, code)
	}
	if answer.State != "confirmed" || answer.Outcome.Task.ID != tk.ID {
		t.Fatalf("the answer = %+v", answer)
	}
	if again, _ := e.plan.GetTicket(e.ctx, tk.ID); again.ArchivedAt == nil || again.ChangedBy == nil || again.ChangedBy.Kind != domain.AuthorProductOwner || again.ChangedBy.ThreadID != threadID {
		t.Fatalf("after confirm the task = %+v, want it archived by the product owner's thread", again)
	}
	if held = e.heldActions(threadID); len(held) != 1 || held[0].State != "confirmed" || e.pendingHeld(threadID) != 0 {
		t.Fatalf("after confirm: %+v pending %d", held, e.pendingHeld(threadID))
	}
	if n := len(e.launches()); n != turns {
		t.Errorf("confirming started a process: launches %d, was %d", n, turns)
	}
	if th := e.thread(threadID); th.Thread.Turn != "idle" {
		t.Errorf("confirming started a turn: %s", th.Thread.Turn)
	}
}

func TestADeclinedHeldActionChangesNothingAndASecondAnswerIsRefused(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Kept")
	e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	id := e.heldActions(threadID)[0].HeldActionID

	var answer struct {
		State string `json:"state"`
	}
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "decline"}, &answer); status != 200 || answer.State != "declined" {
		t.Fatalf("decline = %d %s %+v", status, code, answer)
	}
	if again, _ := e.plan.GetTicket(e.ctx, tk.ID); again.ArchivedAt != nil || again.ChangedBy != nil && again.ChangedBy.Kind == "product_owner" {
		t.Fatalf("a declined archive changed the task: %+v", again)
	}
	// The same click from another tab.
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "confirm"}, nil); status != 409 || code != "HELD_ACTION_ANSWERED" {
		t.Errorf("a second answer = %d %s, want 409 HELD_ACTION_ANSWERED", status, code)
	}
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": "nope", "answer": "confirm"}, nil); status != 404 || code != "HELD_ACTION_NOT_FOUND" {
		t.Errorf("an unknown id = %d %s, want 404 HELD_ACTION_NOT_FOUND", status, code)
	}
	if again, _ := e.plan.GetTicket(e.ctx, tk.ID); again.ArchivedAt != nil {
		t.Error("an answer refused as answered archived the task")
	}
}

func TestAHeldActionStaysPendingThroughARestartAndTwoFollowersSeeTheSameStates(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Later")
	e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	id := e.heldActions(threadID)[0].HeldActionID

	e.restart()
	if held := e.heldActions(threadID); len(held) != 1 || held[0].State != "pending" || e.pendingHeld(threadID) != 1 {
		t.Fatalf("after a restart: %+v pending %d", held, e.pendingHeld(threadID))
	}
	third := followPOFeed(t, e.srv.URL, e.project)
	defer third.stop() // an open feed keeps the test server from closing
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "confirm"}, nil); status != 200 {
		t.Fatalf("confirm after a restart = %d %s", status, code)
	}
	states := func(f *poFeed) string {
		var out []string
		for _, it := range f.of("product_owner_thread_item") {
			if item, ok := it["item"].(map[string]any); ok && item["kind"] == "held_action" {
				out = append(out, item["state"].(string))
			}
		}
		return strings.Join(out, ",")
	}
	eventuallyPO(t, "both followers see the card confirmed", func() string {
		if a, b := states(e.feed), states(third); a != "confirmed" || b != "confirmed" {
			return "first " + a + ", other " + b
		}
		return ""
	})
}

func TestAChangeToATaskThePersonMadeIsHeldWithOldAndNewAndAppliesOnlyOnConfirm(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	var made struct {
		Ticket attributedRow `json:"ticket"`
	}
	e.post("create_ticket", map[string]any{"project_id": e.project, "title": "Mine"}, &made)
	old, err := e.plan.CreateTicket(e.ctx, e.project, "Before attribution", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(e.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE tickets SET created_by = '' WHERE id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}

	for _, tk := range []struct{ id, from, to string }{{made.Ticket.ID, "Mine", "Ours"}, {old.ID, "Before attribution", "After"}} {
		text, refused := e.call(owner, "update_task", map[string]any{"task_id": tk.id, "title": tk.to, "status": "review"})
		if refused || !strings.Contains(text, "Awaiting the person") {
			t.Fatalf("update_task of %s = %q refused=%v, want it held", tk.id, text, refused)
		}
		if got, _ := e.plan.GetTicket(e.ctx, tk.id); got.Title != tk.from || got.Status != domain.TicketStatusBacklog {
			t.Fatalf("a held change applied: %+v", got)
		}
	}
	held := e.heldActions(threadID)
	if len(held) != 2 {
		t.Fatalf("held actions = %+v", held)
	}
	if !strings.Contains(held[0].Summary, `"Mine" → "Ours"`) || !strings.Contains(held[0].Summary, "status backlog → review") {
		t.Errorf("the summary = %q, want old → new for the title and the status", held[0].Summary)
	}
	if e.pendingHeld(threadID) != 2 {
		t.Errorf("pending = %d, want 2", e.pendingHeld(threadID))
	}
	// A move to done is refused whoever made the task, and holds nothing.
	if text, refused := e.call(owner, "update_task", map[string]any{"task_id": made.Ticket.ID, "status": "done"}); !refused || !strings.Contains(text, "Only the person moves a task to done") {
		t.Errorf("a move to done = %q refused=%v", text, refused)
	}
	if len(e.heldActions(threadID)) != 2 {
		t.Error("a move to done was held")
	}

	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": held[0].HeldActionID, "answer": "confirm"}, nil); status != 200 {
		t.Fatalf("confirm = %d %s", status, code)
	}
	got, _ := e.plan.GetTicket(e.ctx, made.Ticket.ID)
	if got.Title != "Ours" || got.Status != domain.TicketStatusReview || got.ChangedBy == nil || got.ChangedBy.Kind != domain.AuthorProductOwner || got.ChangedBy.ThreadID != threadID {
		t.Fatalf("after confirm the task = %+v, want the change by the product owner's thread", got)
	}
	if status, _ := e.post("answer_held_action", map[string]any{"held_action_id": held[1].HeldActionID, "answer": "decline"}, nil); status != 200 {
		t.Fatal("decline failed")
	}
	if got, _ := e.plan.GetTicket(e.ctx, old.ID); got.Title != "Before attribution" {
		t.Errorf("a declined change applied: %+v", got)
	}

	// A task the product owner made changes at once.
	mine := e.madeTask(owner, "create_task", map[string]any{"title": "Product owner's", "parent_id": ""})
	if text, refused := e.call(owner, "update_task", map[string]any{"task_id": mine.ID, "title": "Renamed"}); refused || strings.Contains(text, "Awaiting") {
		t.Errorf("a change to its own task = %q refused=%v, want it applied", text, refused)
	}
}

func TestTheNextTurnCarriesTheAnswersAndConfirmingAloneStartsNoTurn(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Finished")
	e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	id := e.heldActions(threadID)[0].HeldActionID
	e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "decline"}, nil)

	if got := e.received(); strings.Contains(got, "declined") {
		t.Fatalf("the product owner was told before its next turn: %q", got)
	}
	if status, code := e.send(threadID, "what happened?"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(threadID, "idle")
	if got := e.received(); !strings.Contains(got, "declined") || !strings.Contains(got, "Archive the task") || !strings.Contains(got, "what happened?") {
		t.Fatalf("the next turn received %q, want the answer in front of the message", got)
	}
	// It is told once.
	e.send(threadID, "and now?")
	e.waitTurn(threadID, "idle")
	if got := e.received(); strings.Count(got, "declined") != 1 {
		t.Errorf("the answer was given again: %q", got)
	}
	// The person's own message in the thread stays what they wrote.
	for _, m := range e.messages(threadID) {
		if m.Author == "person" && strings.Contains(m.Text, "declined") {
			t.Errorf("the person's message was rewritten: %q", m.Text)
		}
	}
}

func TestAHeldActionNotAnsweredInADayOrAtACheckpointExpiresAndCannotBeConfirmed(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	stale, fresh, checkpointed := e.doneTask("Stale"), e.doneTask("Fresh"), e.doneTask("Checkpointed")

	e.call(owner, "archive_task", map[string]any{"task_id": stale.ID})
	staleID := e.heldActions(threadID)[0].HeldActionID
	// A day and a minute on, the person still has not answered.
	e.po.SetClock(func() time.Time { return time.Now().Add(24*time.Hour + time.Minute) })
	if th := e.thread(threadID); th.Thread.Turn != "idle" {
		t.Fatal(th.Thread.Turn)
	}
	if held := e.heldActions(threadID); held[0].State != "expired" || e.pendingHeld(threadID) != 0 {
		t.Fatalf("after a day: %+v pending %d, want it expired", held, e.pendingHeld(threadID))
	}
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": staleID, "answer": "confirm"}, nil); status != 409 || code != "HELD_ACTION_EXPIRED" {
		t.Errorf("confirm of an expired action = %d %s, want 409 HELD_ACTION_EXPIRED", status, code)
	}
	if again, _ := e.plan.GetTicket(e.ctx, stale.ID); again.ArchivedAt != nil {
		t.Error("an expired action archived the task")
	}

	// The checkpoint expires what waits, whatever its age.
	e.po.SetClock(nil)
	e.call(owner, "archive_task", map[string]any{"task_id": fresh.ID})
	e.call(owner, "archive_task", map[string]any{"task_id": checkpointed.ID})
	if e.pendingHeld(threadID) != 2 {
		t.Fatalf("pending = %d, want 2", e.pendingHeld(threadID))
	}
	if n, err := e.po.ExpirePending(e.ctx, threadID); err != nil || n != 2 {
		t.Fatalf("ExpirePending = %d %v, want 2", n, err)
	}
	if e.pendingHeld(threadID) != 0 {
		t.Errorf("pending after a checkpoint = %d", e.pendingHeld(threadID))
	}
	for _, h := range e.heldActions(threadID) {
		if h.State != "expired" {
			t.Errorf("a held action reads %s after the checkpoint", h.State)
		}
	}
}

// received is what the stand-in claude was sent, one line per message.
func (e *poE2E) received() string {
	b, _ := os.ReadFile(filepath.Join(e.fakeDir, "received"))
	return string(b)
}
