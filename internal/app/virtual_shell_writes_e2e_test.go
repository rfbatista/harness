//go:build !windows

package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The edit proxy's side on the harness: what an agent does to the files of
// its virtual shell is checked, then committed, through the session-scoped
// shell endpoints (contract "Harness server ↔ Agent sessions — virtual
// shell" §4–6).

// shellAnswer is what shell/check and shell/commit answer.
type shellAnswer struct {
	Allow  bool   `json:"allow"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// shellCall posts {op, paths, tool} to the session's shell/<step> endpoint;
// rel paths are made absolute inside the session's shell.
func (e *shellE2E) shellCall(s runningSession, step, op string, rel ...string) (shellAnswer, string) {
	e.t.Helper()
	paths := make([]string, len(rel))
	for i, p := range rel {
		if filepath.IsAbs(p) {
			paths[i] = p
		} else {
			paths[i] = filepath.Join(s.shell, filepath.FromSlash(p))
		}
	}
	b, _ := json.Marshal(map[string]any{"op": op, "paths": paths, "tool": "Write"})
	status, text := postJSON(e.t, e.srv.URL+"/api/sessions/"+s.id+"/shell/"+step, string(b))
	if status != http.StatusOK {
		e.t.Fatalf("shell/%s %s %v = %d %s", step, op, rel, status, text)
	}
	var out shellAnswer
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		e.t.Fatalf("shell/%s answer %s: %v", step, text, err)
	}
	return out, strings.TrimSpace(text)
}

// shellOp checks op, does it on disk, and commits it; both must allow.
func (e *shellE2E) shellOp(s runningSession, op string, do func(), rel ...string) {
	e.t.Helper()
	if a, raw := e.shellCall(s, "check", op, rel...); !a.Allow {
		e.t.Fatalf("check %s %v refused: %s", op, rel, raw)
	}
	do()
	if a, raw := e.shellCall(s, "commit", op, rel...); !a.Allow {
		e.t.Fatalf("commit %s %v refused: %s", op, rel, raw)
	}
}

func writeShellFile(t *testing.T, s runningSession, rel, content string) {
	t.Helper()
	p := filepath.Join(s.shell, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func moveShellFile(t *testing.T, s runningSession, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(s.shell, filepath.FromSlash(from)), filepath.Join(s.shell, filepath.FromSlash(to))); err != nil {
		t.Fatal(err)
	}
}

func removeShellFile(t *testing.T, s runningSession, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(s.shell, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

// indexedID is the id .index.json names for rel in the session's shell.
func indexedID(t *testing.T, s runningSession, rel string) string {
	t.Helper()
	idx, err := shellIndex(s.shell)
	if err != nil {
		t.Fatal(err)
	}
	row, ok := idx[rel]
	if !ok || row.ID == "" {
		t.Fatalf("%s is not indexed with an id: %+v", rel, idx)
	}
	return row.ID
}

// document is the stored document, or nil when there is none.
func (e *shellE2E) document(id string) *domain.Document {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/get_document?document_id=" + url.QueryEscape(id))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	b, _ := io.ReadAll(res.Body)
	var out struct {
		Document domain.Document `json:"document"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil {
		e.t.Fatalf("get_document = %d %s", res.StatusCode, b)
	}
	return &out.Document
}

func (e *shellE2E) linkedTo(ticketID, docID string) bool {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/list_ticket_documents?ticket_id=" + url.QueryEscape(ticketID))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Documents []domain.Document `json:"documents"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		e.t.Fatal(err)
	}
	for _, d := range out.Documents {
		if d.ID == docID {
			return true
		}
	}
	return false
}

func wantDocument(t *testing.T, d *domain.Document, title string, format domain.DocumentFormat, scope domain.DocumentScope, content string) {
	t.Helper()
	switch {
	case d == nil:
		t.Fatalf("document %q is not stored", title)
	case d.Title != title || d.Format != format || d.Scope != scope || d.Content != content:
		t.Fatalf("stored %q %s %s %q, want %q %s %s %q", d.Title, d.Format, d.Scope, d.Content, title, format, scope, content)
	}
}

// Files an agent creates in task/docs and project/docs become documents
// linked to its task, titled from their first heading or else their name,
// in the format their extension says, never refused as not HTML; they keep
// the names the agent chose, and every other running shell that should see
// them gets them.
func TestFilesCreatedInTheShellBecomeDocuments(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Shell writes"), e.task("Elsewhere")
	a, peer, b := e.start(taskA), e.start(taskA), e.start(taskB)

	md := "# Shell notes\n\nwritten in the shell\n"
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/notes.md", md) }, "task/docs/notes.md")
	notes := indexedID(t, a, "task/docs/notes.md")
	wantDocument(t, e.document(notes), "Shell notes", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, md)
	if !e.linkedTo(taskA, notes) {
		t.Fatalf("the created task document is not linked to the task")
	}

	page := "<title>Fragment page</title><p>not a complete page</p>"
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/page.html", page) }, "task/docs/page.html")
	pageID := indexedID(t, a, "task/docs/page.html")
	wantDocument(t, e.document(pageID), "Fragment page", domain.DocumentFormatHTML, domain.DocumentScopeTask, page)

	txt := "plain words, no heading\n"
	e.shellOp(a, "create", func() { writeShellFile(t, a, "project/docs/plain-words.txt", txt) }, "project/docs/plain-words.txt")
	plain := indexedID(t, a, "project/docs/plain-words.txt")
	wantDocument(t, e.document(plain), "plain-words", domain.DocumentFormatText, domain.DocumentScopeProject, txt)
	if !e.linkedTo(taskA, plain) {
		t.Fatalf("the created project document is not linked to the task")
	}

	eventually(t, "the peer's shell gets the task documents", func() error {
		if err := hasFile(peer.shell, "task/docs/shell-notes.md", md, notes, "task", domain.DocumentVersion(md)); err != nil {
			return err
		}
		return hasFile(peer.shell, "task/docs/fragment-page.html", page, pageID, "task", "")
	})
	for name, s := range map[string]runningSession{"peer": peer, "B": b} {
		eventually(t, "the project document in "+name, func() error {
			return hasFile(s.shell, "project/docs/plain-words.txt", txt, plain, "project", "")
		})
	}
	if err := lacks(b.shell, notes); err != nil {
		t.Fatalf("another task's shell got the task document: %v", err)
	}

	// The writer's shell keeps the names it chose, with no second copy.
	if err := hasFile(a.shell, "task/docs/notes.md", md, notes, "task", domain.DocumentVersion(md)); err != nil {
		t.Fatal(err)
	}
	idx, _ := shellIndex(a.shell)
	for p, row := range idx {
		if (row.ID == notes && p != "task/docs/notes.md") || (row.ID == pageID && p != "task/docs/page.html") {
			t.Fatalf("a second copy at %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(a.shell, "task", "docs", "shell-notes.md")); err == nil {
		t.Fatalf("the writer's shell got a slug-named copy of its own file")
	}
}

// Modifying, renaming, moving between task/docs and project/docs, and
// removing a document file store the change, and the other running shells
// get it.
func TestDocumentFilesAreChangedMovedAndRemovedFromTheShell(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Shell writes"), e.task("Elsewhere")
	a, peer, b := e.start(taskA), e.start(taskA), e.start(taskB)

	doc := e.createDocument("Plan", "# Plan\n\nv1\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in A", func() error { return hasFile(a.shell, "task/docs/plan.md", "# Plan\n\nv1\n", doc.ID, "task", "") })

	// Modify: the content is stored, the title kept.
	v2 := "# A different heading\n\nv2\n"
	e.shellOp(a, "modify", func() { writeShellFile(t, a, "task/docs/plan.md", v2) }, "task/docs/plan.md")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, v2)
	if err := hasFile(a.shell, "task/docs/plan.md", v2, doc.ID, "task", domain.DocumentVersion(v2)); err != nil {
		t.Fatalf("the writer's base version: %v", err)
	}
	eventually(t, "the modified plan in the peer", func() error {
		return hasFile(peer.shell, "task/docs/plan.md", v2, doc.ID, "task", domain.DocumentVersion(v2))
	})

	// Rename within the folder: title unchanged, index follows.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "task/docs/plan.md", "task/docs/the-plan.md") }, "task/docs/plan.md", "task/docs/the-plan.md")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, v2)
	if err := hasFile(a.shell, "task/docs/the-plan.md", v2, doc.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
	if err := lacks(a.shell, "no-such-id", "task/docs/plan.md"); err != nil {
		t.Fatal(err)
	}

	// A new extension is a new format.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "task/docs/the-plan.md", "task/docs/the-plan.txt") }, "task/docs/the-plan.md", "task/docs/the-plan.txt")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatText, domain.DocumentScopeTask, v2)

	// To project/docs: refused; a person moves documents to project level.
	if got, _ := e.shellCall(a, "check", "mv", "task/docs/the-plan.txt", "project/docs/the-plan.txt"); got != projectMoveDenied("task/docs/the-plan.txt") {
		t.Fatalf("check mv to project/docs = %+v", got)
	}
	// Moved anyway, the commit is refused too and the file put back.
	moveShellFile(t, a, "task/docs/the-plan.txt", "project/docs/the-plan.txt")
	if got, _ := e.shellCall(a, "commit", "mv", "task/docs/the-plan.txt", "project/docs/the-plan.txt"); got != projectMoveDenied("task/docs/the-plan.txt") {
		t.Fatalf("commit mv to project/docs = %+v", got)
	}
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatText, domain.DocumentScopeTask, v2)
	if err := hasFile(a.shell, "task/docs/the-plan.txt", v2, doc.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.shell, "project", "docs", "the-plan.txt")); err == nil {
		t.Fatal("the refused move left the plan in project/docs")
	}

	// Moved anyway over a project document, that document is kept.
	terms := e.createDocument("Terms", "terms", "text", "project")
	eventually(t, "the terms in A", func() error { return hasFile(a.shell, "project/docs/terms.txt", "terms", terms.ID, "project", "") })
	moveShellFile(t, a, "task/docs/the-plan.txt", "project/docs/terms.txt")
	if got, _ := e.shellCall(a, "commit", "mv", "task/docs/the-plan.txt", "project/docs/terms.txt"); got != projectMoveDenied("task/docs/the-plan.txt") {
		t.Fatalf("commit mv over a project document = %+v", got)
	}
	if d := e.document(terms.ID); d == nil || d.Content != "terms" {
		t.Fatalf("the project document written over: %+v", d)
	}
	if err := hasFile(a.shell, "project/docs/terms.txt", "terms", terms.ID, "project", ""); err != nil {
		t.Fatal(err)
	}
	if err := hasFile(a.shell, "task/docs/the-plan.txt", v2, doc.ID, "task", ""); err != nil {
		t.Fatal(err)
	}

	// The person moves it: project scope, in every shell of the project.
	e.post("set_document_scope", map[string]string{"document_id": doc.ID, "scope": "project"}, nil)
	eventually(t, "the moved plan in A", func() error { return hasFile(a.shell, "project/docs/plan.txt", v2, doc.ID, "project", "") })
	eventually(t, "the moved plan in B", func() error { return hasFile(b.shell, "project/docs/plan.txt", v2, doc.ID, "project", "") })

	// Back to task/docs: it is linked to this task, so it may.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "project/docs/plan.txt", "task/docs/the-plan.txt") }, "project/docs/plan.txt", "task/docs/the-plan.txt")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatText, domain.DocumentScopeTask, v2)
	eventually(t, "the plan gone from B", func() error { return lacks(b.shell, doc.ID, "project/docs/plan.txt") })

	// mkdir under docs is allowed.
	e.shellOp(a, "mkdir", func() {
		if err := os.Mkdir(filepath.Join(a.shell, "task", "docs", "drafts"), 0o755); err != nil {
			t.Fatal(err)
		}
	}, "task/docs/drafts")

	// A folder holding a document cannot move to project/docs either.
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/drafts/note.md", "# Note\n") }, "task/docs/drafts/note.md")
	if got, _ := e.shellCall(a, "check", "mv", "task/docs/drafts", "project/docs/drafts"); got != projectMoveDenied("task/docs/drafts") {
		t.Fatalf("check mv a folder to project/docs = %+v", got)
	}
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/drafts/more.md", "# More\n") }, "task/docs/drafts/more.md")
	moveShellFile(t, a, "task/docs/drafts", "project/docs/drafts")
	if got, _ := e.shellCall(a, "commit", "mv", "task/docs/drafts", "project/docs/drafts"); got != projectMoveDenied("task/docs/drafts") {
		t.Fatalf("commit mv a folder to project/docs = %+v", got)
	}
	for _, name := range []string{"note.md", "more.md"} {
		if _, err := os.Stat(filepath.Join(a.shell, "task", "docs", "drafts", name)); err != nil {
			t.Fatalf("the refused folder move did not put %s back: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(a.shell, "project", "docs", "drafts")); err == nil {
		t.Fatal("the refused folder move left project/docs/drafts")
	}

	// rm deletes the document, and it leaves every shell.
	e.shellOp(a, "rm", func() { removeShellFile(t, a, "task/docs/the-plan.txt") }, "task/docs/the-plan.txt")
	if d := e.document(doc.ID); d != nil {
		t.Fatalf("the removed document is still stored: %+v", d)
	}
	if err := lacks(a.shell, doc.ID, "task/docs/the-plan.txt"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the removed plan gone from the peer", func() error { return lacks(peer.shell, doc.ID, "task/docs/plan.txt") })
}

// projectMoveDenied is the refusal of moving the document at rel to project
// level.
func projectMoveDenied(rel string) shellAnswer {
	return shellAnswer{Code: "PROJECT_MOVE_DENIED", Reason: "PROJECT_MOVE_DENIED: " + rel + ` cannot move to project scope: a person moves documents to project level ("Move to project" on the task's documents page); nothing was written`}
}

// A write made on a copy the harness has changed since is refused with
// BASE_VERSION_STALE naming the current version, and the shell's file is
// the stored one afterwards; redone, it is stored.
func TestAStaleWriteIsRefusedAndTheFileRefreshed(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell writes")
	a := e.start(taskA)

	doc := e.createDocument("Plan", "# Plan\n\nv1\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in A", func() error { return hasFile(a.shell, "task/docs/plan.md", "# Plan\n\nv1\n", doc.ID, "task", "") })

	// The agent is about to write when the person saves the page.
	if ans, raw := e.shellCall(a, "check", "write", "task/docs/plan.md"); !ans.Allow {
		t.Fatalf("check refused: %s", raw)
	}
	person := "# Plan\n\nthe person's v2\n"
	e.post("update_document", map[string]string{"document_id": doc.ID, "title": "Plan", "content": person}, nil)
	writeShellFile(t, a, "task/docs/plan.md", "# Plan\n\nthe agent's v2\n")

	ans, _ := e.shellCall(a, "commit", "write", "task/docs/plan.md")
	now := domain.DocumentVersion(person)
	want := shellAnswer{Code: "BASE_VERSION_STALE", Reason: "BASE_VERSION_STALE: task/docs/plan.md changed since this session last received it (now " + now + "); the file has been refreshed — read it again and redo the change"}
	if ans != want {
		t.Fatalf("commit = %+v, want %+v", ans, want)
	}
	if d := e.document(doc.ID); d.Content != person {
		t.Fatalf("the stale write was stored: %q", d.Content)
	}
	if err := hasFile(a.shell, "task/docs/plan.md", person, doc.ID, "task", now); err != nil {
		t.Fatalf("the shell's file after the refusal: %v", err)
	}

	// Redone on the refreshed copy, it is stored.
	redone := "# Plan\n\nthe person's v2, and the agent's line\n"
	e.shellOp(a, "write", func() { writeShellFile(t, a, "task/docs/plan.md", redone) }, "task/docs/plan.md")
	if d := e.document(doc.ID); d.Content != redone {
		t.Fatalf("the redone write: %q", d.Content)
	}
}

// Writes to the read-only files are refused with READ_ONLY, an unlinked
// project document cannot move to task/docs (NOT_ON_TASK), and paths
// outside the shell get the empty answer.
func TestShellRefusals(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell writes")
	e.upload(taskA, "mockup.txt", "the mockup")
	a := e.start(taskA)
	eventually(t, "the resource in A", func() error { return fileWith(a.shell, "task/resources/mockup.txt", "the mockup") })

	readOnly := func(p string) shellAnswer {
		return shellAnswer{Code: "READ_ONLY", Reason: "READ_ONLY: " + p + " is read-only (task.json, resources, index, README, attached.md, urls.md)"}
	}
	for _, p := range []string{"task/task.json", "task/resources/mockup.txt", "task/resources/links.md", ".index.json", "README.md", "task/designs/attached.md", "project/designs/urls.md"} {
		for _, step := range []string{"check", "commit"} {
			if got, _ := e.shellCall(a, step, "write", p); got != readOnly(p) {
				t.Errorf("%s write %s = %+v, want %+v", step, p, got, readOnly(p))
			}
		}
		if got, _ := e.shellCall(a, "check", "rm", p); got != readOnly(p) {
			t.Errorf("rm %s = %+v", p, got)
		}
	}
	if got, _ := e.shellCall(a, "check", "mv", "task/resources/mockup.txt", "task/docs/mockup.txt"); got != readOnly("task/resources/mockup.txt") {
		t.Errorf("mv out of resources = %+v", got)
	}
	if got, _ := e.shellCall(a, "check", "write", "task/resources/new.txt"); got != readOnly("task/resources/new.txt") {
		t.Errorf("a new file in resources = %+v", got)
	}

	// A project document not linked to this task stays in project scope.
	glossary := e.createDocument("Glossary", "terms", "text", "project")
	eventually(t, "the glossary in A", func() error {
		return hasFile(a.shell, "project/docs/glossary.txt", "terms", glossary.ID, "project", "")
	})
	want := shellAnswer{Code: "NOT_ON_TASK", Reason: "NOT_ON_TASK: project/docs/glossary.txt cannot move to task scope: the document is not linked to this task"}
	if got, _ := e.shellCall(a, "check", "mv", "project/docs/glossary.txt", "task/docs/glossary.txt"); got != want {
		t.Fatalf("mv an unlinked document to task/docs = %+v, want %+v", got, want)
	}
	if d := e.document(glossary.ID); d.Scope != domain.DocumentScopeProject {
		t.Fatalf("the glossary moved to %s", d.Scope)
	}

	// Outside the shell: the empty answer, whatever the operation.
	outside := filepath.Join(a.worktree, "main.go")
	for _, op := range []string{"write", "rm", "mkdir"} {
		for _, step := range []string{"check", "commit"} {
			if _, raw := e.shellCall(a, step, op, outside); raw != `{"allow":true}` {
				t.Errorf("%s %s outside = %s", step, op, raw)
			}
		}
	}
	if _, raw := e.shellCall(a, "check", "mv", outside, filepath.Join(a.worktree, "other.go")); raw != `{"allow":true}` {
		t.Errorf("mv outside = %s", raw)
	}
}
