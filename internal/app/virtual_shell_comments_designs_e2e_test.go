//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Comments and designs written through the edit proxy (contract "Harness
// server ↔ Agent sessions — virtual shell" §4, §6).

// comments is the task's comments as the API lists them, by id.
func (e *shellE2E) comments(ticketID string) map[string]domain.TicketComment {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/list_ticket_comments?ticket_id=" + url.QueryEscape(ticketID))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out struct {
		Comments []domain.TicketComment `json:"comments"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil {
		e.t.Fatalf("list_ticket_comments = %d %s", res.StatusCode, b)
	}
	byID := map[string]domain.TicketComment{}
	for _, c := range out.Comments {
		byID[c.ID] = c
	}
	return byID
}

// A file written in task/comments is a comment by this session; editing it
// updates the comment, removing it deletes it, and every other shell of
// the task follows.
func TestACommentIsWrittenEditedAndRemovedThroughTheShell(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell comments")
	a, peer := e.start(taskA), e.start(taskA)

	rel := "task/comments/my-note.md"
	e.shellOp(a, "write", func() { writeShellFile(t, a, rel, "Names are stable now.") }, rel)
	id := indexedID(t, a, rel)
	c, ok := e.comments(taskA)[id]
	if !ok || c.Body != "Names are stable now." || c.Author != domain.CommentBySession || c.SessionID != a.id {
		t.Fatalf("stored comment = %+v (found %v), want a's comment with the file's body", c, ok)
	}
	if err := hasFile(a.shell, rel, "Names are stable now.", id, "task", ""); err != nil {
		t.Fatalf("the writer's shell: %v", err)
	}
	peerPath := "task/comments/" + c.CreatedAt.UTC().Format("20060102-150405") + "-claude-" + id[:8] + ".md"
	eventually(t, "the comment in the peer's shell", func() error {
		return hasFile(peer.shell, peerPath, "Names are stable now.", id, "task", "")
	})

	e.shellOp(a, "modify", func() { writeShellFile(t, a, rel, "Names and paths are stable now.") }, rel)
	if got := e.comments(taskA)[id].Body; got != "Names and paths are stable now." {
		t.Fatalf("edited comment body = %q", got)
	}
	eventually(t, "the edited comment in the peer's shell", func() error {
		return hasFile(peer.shell, peerPath, "Names and paths are stable now.", id, "task", "")
	})

	e.shellOp(a, "rm", func() { removeShellFile(t, a, rel) }, rel)
	if _, ok := e.comments(taskA)[id]; ok {
		t.Fatalf("the removed comment is still stored")
	}
	if err := lacks(a.shell, id, rel); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the comment gone from the peer's shell", func() error { return lacks(peer.shell, id, peerPath) })
}

// artifact is the stored artifact, or nil when there is none.
func (e *shellE2E) artifact(id string) *domain.Artifact {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/artifacts/" + url.PathEscape(id))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	b, _ := io.ReadAll(res.Body)
	var a domain.Artifact
	if res.StatusCode != http.StatusOK || json.Unmarshal(b, &a) != nil {
		e.t.Fatalf("get artifact = %d %s", res.StatusCode, b)
	}
	return &a
}

// served is what the artifact's view route serves for its own file.
func (e *shellE2E) served(id string) (int, string) {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/artifacts/" + url.PathEscape(id) + "/view/")
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func wantServed(t *testing.T, e *shellE2E, id, content string) {
	t.Helper()
	if status, body := e.served(id); status != http.StatusOK || body != content {
		t.Fatalf("artifact %s serves %d %q, want %q", id, status, body, content)
	}
}

// A file created in task/designs is published on the task, changed it is
// revised, and its bytes are served from the harness's copy even once the
// shell is gone; mv to project/designs makes it a project asset in every
// shell of the project, and back makes it the task's again; rm unpublishes
// it. A design in a nested folder keeps its relative path.
func TestDesignsArePublishedRevisedMovedAndUnpublishedThroughTheShell(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Shell designs"), e.task("Elsewhere")
	a, peer, b := e.start(taskA), e.start(taskA), e.start(taskB)

	rel := "task/designs/mock/card.html"
	v1 := "<title>Card mock</title><h1>v1</h1>"
	e.shellOp(a, "mkdir", func() {
		if err := os.MkdirAll(filepath.Join(a.shell, "task", "designs", "mock"), 0o755); err != nil {
			t.Fatal(err)
		}
	}, "task/designs/mock")
	e.shellOp(a, "write", func() { writeShellFile(t, a, rel, v1) }, rel)
	id := indexedID(t, a, rel)
	art := e.artifact(id)
	if art == nil || art.Title != "Card mock" || art.TicketID != taskA || art.Scope != domain.ArtifactScopeTask || art.Revision != 1 || art.Kind != domain.ArtifactPage || art.Path != "mock/card.html" {
		t.Fatalf("published artifact = %+v", art)
	}
	wantServed(t, e, id, v1)
	if err := hasFile(a.shell, rel, v1, id, "task", "1"); err != nil {
		t.Fatalf("the writer's shell: %v", err)
	}
	eventually(t, "the design at its relative path in the peer", func() error { return hasFile(peer.shell, rel, v1, id, "task", "1") })
	if err := lacks(b.shell, id); err != nil {
		t.Fatalf("another task's shell got the task design: %v", err)
	}

	v2 := "<title>Card mock</title><h1>v2</h1>"
	e.shellOp(a, "modify", func() { writeShellFile(t, a, rel, v2) }, rel)
	if art := e.artifact(id); art.Revision != 2 {
		t.Fatalf("revised artifact = %+v", art)
	}
	wantServed(t, e, id, v2)
	eventually(t, "the revision in the peer", func() error { return hasFile(peer.shell, rel, v2, id, "task", "2") })

	// To project/designs: a project asset, in every shell of the project.
	e.shellOp(a, "mv", func() {
		if err := os.MkdirAll(filepath.Join(a.shell, "project", "designs", "mock"), 0o755); err != nil {
			t.Fatal(err)
		}
		moveShellFile(t, a, rel, "project/designs/mock/card.html")
	}, rel, "project/designs/mock/card.html")
	if art := e.artifact(id); art.Scope != domain.ArtifactScopeProject {
		t.Fatalf("moved artifact scope = %s", art.Scope)
	}
	if err := hasFile(a.shell, "project/designs/mock/card.html", v2, id, "project", "2"); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]runningSession{"peer": peer, "B": b} {
		eventually(t, "the project asset in "+name, func() error {
			if err := lacks(s.shell, "no-such-id", rel); err != nil {
				return err
			}
			return hasFile(s.shell, "project/designs/mock/card.html", v2, id, "project", "")
		})
	}

	// And back to the task it was published on.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "project/designs/mock/card.html", rel) }, "project/designs/mock/card.html", rel)
	if art := e.artifact(id); art.Scope != domain.ArtifactScopeTask {
		t.Fatalf("moved-back artifact scope = %s", art.Scope)
	}
	eventually(t, "the design gone from B", func() error { return lacks(b.shell, id, "project/designs/mock/card.html") })

	// The shell goes with its session; the bytes stay.
	if _, err := e.orch.EndInteractive(e.ctx, a.id, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.shell); !os.IsNotExist(err) {
		t.Fatalf("the ended session's shell is still there: %v", err)
	}
	wantServed(t, e, id, v2)

	// rm in another shell of the task unpublishes it.
	eventually(t, "the design back in the peer", func() error { return hasFile(peer.shell, rel, v2, id, "task", "") })
	e.shellOp(peer, "rm", func() { removeShellFile(t, peer, rel) }, rel)
	if art := e.artifact(id); art != nil {
		t.Fatalf("the removed design is still published: %+v", art)
	}
	if status, _ := e.served(id); status != http.StatusNotFound {
		t.Fatalf("the unpublished design is still served: %d", status)
	}
}

// The person's comment and another session's are someone else's: editing
// or removing them is refused with COMMENT_NOT_YOURS naming the author,
// never READ_ONLY, at check and at commit, and a commit that went ahead
// anyway leaves the stored comment in the file.
func TestOthersCommentsAreRefusedAsNotYours(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell comments")
	a, peer := e.start(taskA), e.start(taskA)

	var added struct {
		Comment domain.TicketComment `json:"comment"`
	}
	e.post("add_ticket_comment", map[string]string{"ticket_id": taskA, "body": "From the person."}, &added)
	person := added.Comment
	personPath := "task/comments/" + person.CreatedAt.UTC().Format("20060102-150405") + "-person-" + person.ID[:8] + ".md"

	e.shellOp(peer, "write", func() { writeShellFile(t, peer, "task/comments/peer.md", "From the peer.") }, "task/comments/peer.md")
	peerID := indexedID(t, peer, "task/comments/peer.md")
	peerPath := "task/comments/" + e.comments(taskA)[peerID].CreatedAt.UTC().Format("20060102-150405") + "-claude-" + peerID[:8] + ".md"

	for _, c := range []struct{ rel, id, author, body string }{
		{personPath, person.ID, "person", "From the person."},
		{peerPath, peerID, "claude", "From the peer."},
	} {
		eventually(t, "the comment in A", func() error { return hasFile(a.shell, c.rel, c.body, c.id, "task", "") })
		info, err := os.Stat(filepath.Join(a.shell, filepath.FromSlash(c.rel)))
		if err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("%s mode = %v (%v), want 0444", c.rel, info, err)
		}
		want := shellAnswer{Code: "COMMENT_NOT_YOURS", Reason: "COMMENT_NOT_YOURS: " + c.rel + " was written by " + c.author}
		for _, op := range []string{"write", "rm"} {
			if got, _ := e.shellCall(a, "check", op, c.rel); got != want {
				t.Errorf("check %s %s = %+v, want %+v", op, c.rel, got, want)
			}
		}

		// The write went ahead anyway: refused, and the file is the stored one.
		abs := filepath.Join(a.shell, filepath.FromSlash(c.rel))
		_ = os.Chmod(abs, 0o644)
		writeShellFile(t, a, c.rel, "Overwritten.")
		if got, _ := e.shellCall(a, "commit", "write", c.rel); got != want {
			t.Errorf("commit write %s = %+v, want %+v", c.rel, got, want)
		}
		if got := e.comments(taskA)[c.id].Body; got != c.body {
			t.Fatalf("someone else's comment was changed to %q", got)
		}
		eventually(t, "the stored comment back in the file", func() error { return hasFile(a.shell, c.rel, c.body, c.id, "task", "") })

		removeShellFile(t, a, c.rel)
		if got, _ := e.shellCall(a, "commit", "rm", c.rel); got != want {
			t.Errorf("commit rm %s = %+v, want %+v", c.rel, got, want)
		}
		if _, ok := e.comments(taskA)[c.id]; !ok {
			t.Fatalf("someone else's comment was deleted")
		}
		eventually(t, "the removed comment file back", func() error { return hasFile(a.shell, c.rel, c.body, c.id, "task", "") })
	}
}

// Changing the task's title, description or status rewrites task/task.json
// in its running shells.
func TestTaskChangesRefreshTaskJSON(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell task")
	a := e.start(taskA)

	taskJSON := func(title, description, status string) func() error {
		return func() error {
			b, err := os.ReadFile(filepath.Join(a.shell, "task", "task.json"))
			if err != nil {
				return err
			}
			var got struct{ ID, Title, Description, Status string }
			if err := json.Unmarshal(b, &got); err != nil {
				return err
			}
			if got.ID != taskA || got.Title != title || got.Description != description || got.Status != status {
				return fmt.Errorf("task.json = %s, want %q %q %q", b, title, description, status)
			}
			return nil
		}
	}
	eventually(t, "task.json at start", taskJSON("Shell task", "", "in_progress"))

	e.post("update_ticket", map[string]string{"ticket_id": taskA, "title": "Renamed task"}, nil)
	eventually(t, "the new title", taskJSON("Renamed task", "", "in_progress"))
	e.post("update_ticket", map[string]string{"ticket_id": taskA, "description": "What it is for."}, nil)
	eventually(t, "the new description", taskJSON("Renamed task", "What it is for.", "in_progress"))
	e.post("update_ticket", map[string]string{"ticket_id": taskA, "status": "review"}, nil)
	eventually(t, "the new status", taskJSON("Renamed task", "What it is for.", "review"))
}

// A url artifact published on the task is listed in task/designs/urls.md,
// read-only, and writes to that file are refused with READ_ONLY.
func TestTaskDesignURLsAreListedReadOnly(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Shell designs")
	a, peer := e.start(taskA), e.start(taskA)

	callTask(t, e.dial(peer), "publish_artifact", map[string]any{"url": "http://localhost:5173/", "title": "Dev server"})
	eventually(t, "the url in task/designs/urls.md", func() error {
		return fileWith(a.shell, "task/designs/urls.md", "[Dev server](http://localhost:5173/)")
	})
	info, err := os.Stat(filepath.Join(a.shell, "task", "designs", "urls.md"))
	if err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("task/designs/urls.md mode = %v (%v), want 0444", info, err)
	}
	if _, ok := mustIndex(t, a)["task/designs/urls.md"]; !ok {
		t.Fatalf("task/designs/urls.md is not indexed")
	}
	want := shellAnswer{Code: "READ_ONLY", Reason: "READ_ONLY: task/designs/urls.md is read-only (task.json, resources, index, README, attached.md, urls.md)"}
	for _, op := range []string{"write", "rm"} {
		for _, step := range []string{"check", "commit"} {
			if got, _ := e.shellCall(a, step, op, "task/designs/urls.md"); got != want {
				t.Errorf("%s %s = %+v, want %+v", step, op, got, want)
			}
		}
	}
}

func mustIndex(t *testing.T, s runningSession) map[string]ports.ShellFile {
	t.Helper()
	idx, err := shellIndex(s.shell)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
