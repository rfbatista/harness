//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The reviewer's gaps in child 2's server part: deletes the product owner's
// shell must refuse beyond rm, two answers at the same instant, and an answer
// the product owner must learn however long the thread grew since it held
// the action.

func TestTheProductOwnersShellRefusesMovingADocumentOutOfTheShell(t *testing.T) {
	e := newPOE2E(t)
	threadID, _ := e.ownerSession()
	sid := e.thread(threadID).Thread.SessionID
	if ans := e.ownerShellWrite(threadID, "project/docs/keep.md", "# Keep\n"); !ans.Allow {
		t.Fatalf("the write = %+v", ans)
	}
	dir, _ := e.shells.RootDir(sid)
	keep := filepath.Join(dir, "project/docs/keep.md")
	out := filepath.Join(t.TempDir(), "keep.md")

	// A move out of the shell takes the document away from the project: a delete.
	mv := ports.ShellEdit{Op: "mv", Paths: []string{keep, out}}
	if ans := e.shells.CheckShellEdit(e.ctx, sid, mv); ans.Allow || ans.Code != "PRODUCT_OWNER_NO_DELETE" {
		t.Errorf("mv out of the shell, check = %+v, want PRODUCT_OWNER_NO_DELETE", ans)
	}
	if err := os.Rename(keep, out); err != nil {
		t.Fatal(err)
	}
	if ans := e.shells.CommitShellEdit(e.ctx, sid, mv); ans.Allow || ans.Code != "PRODUCT_OWNER_NO_DELETE" {
		t.Errorf("mv out of the shell, commit = %+v, want PRODUCT_OWNER_NO_DELETE", ans)
	}
	if e.documentByTitle("Keep") == nil {
		t.Error("a move out of the shell deleted the document")
	}
}

func TestTheProductOwnersShellRefusesAMoveOntoAnotherDocument(t *testing.T) {
	e := newPOE2E(t)
	threadID, _ := e.ownerSession()
	sid := e.thread(threadID).Thread.SessionID
	for _, rel := range []string{"project/docs/a.md", "project/docs/b.md"} {
		if ans := e.ownerShellWrite(threadID, rel, "# "+strings.TrimSuffix(filepath.Base(rel), ".md")+"\n"); !ans.Allow {
			t.Fatalf("the write of %s = %+v", rel, ans)
		}
	}
	dir, _ := e.shells.RootDir(sid)
	mv := ports.ShellEdit{Op: "mv", Paths: []string{filepath.Join(dir, "project/docs/a.md"), filepath.Join(dir, "project/docs/b.md")}}
	if ans := e.shells.CheckShellEdit(e.ctx, sid, mv); ans.Allow || ans.Code != "PRODUCT_OWNER_NO_DELETE" {
		t.Errorf("mv onto a document = %+v, want PRODUCT_OWNER_NO_DELETE", ans)
	}
	if e.documentByTitle("a") == nil || e.documentByTitle("b") == nil {
		t.Error("a refused move-over lost a document")
	}
}

func TestTwoAnswersAtTheSameInstantApplyOnceAndTheOtherIsAnswered(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Raced")
	e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	id := e.heldActions(threadID)[0].HeldActionID

	var wg sync.WaitGroup
	statuses, codes := make([]int, 2), make([]string, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], codes[i] = e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "confirm"}, nil)
		}()
	}
	wg.Wait()
	ok, answered := 0, 0
	for i := range 2 {
		switch {
		case statuses[i] == 200:
			ok++
		case statuses[i] == 409 && codes[i] == "HELD_ACTION_ANSWERED":
			answered++
		}
	}
	if ok != 1 || answered != 1 {
		t.Fatalf("two tabs confirming at once = %v %v, want one 200 and one 409 HELD_ACTION_ANSWERED", statuses, codes)
	}
	if e.pendingHeld(threadID) != 0 {
		t.Errorf("pending after the race = %d", e.pendingHeld(threadID))
	}
}

func TestTheNextTurnCarriesAnAnswerToAnActionHeldMoreThanAPageAgo(t *testing.T) {
	e := newPOE2E(t)
	threadID, owner := e.ownerSession()
	tk := e.doneTask("Long ago")
	e.call(owner, "archive_task", map[string]any{"task_id": tk.ID})
	id := e.heldActions(threadID)[0].HeldActionID

	// The thread goes on: a turn's tool calls each add an activity line.
	for i := range domain.ProductOwnerThreadPage + 10 {
		if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: threadID, Kind: domain.ProductOwnerItemActivity, At: time.Now(), Text: "Reading a task " + string(rune('a'+i%26))}); err != nil {
			t.Fatal(err)
		}
	}
	if status, code := e.post("answer_held_action", map[string]any{"held_action_id": id, "answer": "decline"}, nil); status != 200 {
		t.Fatalf("decline = %d %s", status, code)
	}
	if status, code := e.send(threadID, "what happened?"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(threadID, "idle")
	if got := e.receivedText(); !strings.Contains(got, "declined") || !strings.Contains(got, "Long ago") {
		t.Errorf("the next turn received %q, want the answer to the action held a page ago", got)
	}
}

func TestATurnWithNoAnswersToTellCarriesNoAnswersHeader(t *testing.T) {
	e := newPOE2E(t)
	threadID, _ := e.ownerSession()
	if status, code := e.send(threadID, "and next?"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(threadID, "idle")
	if got := e.receivedText(); strings.Contains(got, "Since your last turn") {
		t.Errorf("a turn with nothing answered received the answers header: %q", got)
	}
}
