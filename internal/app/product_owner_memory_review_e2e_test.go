package app

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"

	"operators-mcp/internal/domain"
)

// The review's tests of the product owner's memory (ADR 0111), at the wired
// server over the stand-in claude: the change note across several changes of
// one entry, interleaved threads, a restart and a running turn; the exact
// length limit; and another project's entry ids.

// updateMemory edits an entry as the person does on the Memory tab.
func (e *poE2E) updateMemory(entryID, text string) {
	e.t.Helper()
	if status, code := e.post("update_memory_entry", map[string]any{"entry_id": entryID, "text": text}, nil); status != 200 {
		e.t.Fatalf("update %s = %d %s", entryID, status, code)
	}
}

// deleteMemory deletes an entry as the person does on the Memory tab.
func (e *poE2E) deleteMemory(entryID string) {
	e.t.Helper()
	if status, code := e.post("delete_memory_entry", map[string]any{"entry_id": entryID}, nil); status != 200 {
		e.t.Fatalf("delete %s = %d %s", entryID, status, code)
	}
}

// otherProjectOwner starts a thread in a second project and dials its session.
func (e *poE2E) otherProjectOwner() (projectID, threadID string, owner *client.Client) {
	e.t.Helper()
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", e.t.TempDir())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.cat.Projects.CreateRepository(e.ctx, other.ID, "other", "", "https://example.com/other.git", e.t.TempDir()); err != nil {
		e.t.Fatal(err)
	}
	var started struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": other.ID, "message": "hi"}, &started); status != 200 {
		e.t.Fatalf("start in other project = %d %s", status, code)
	}
	return other.ID, started.ThreadID, e.dialSession(e.waitTurn(started.ThreadID, "idle").Thread.SessionID)
}

func TestAnEditThenADeleteOfOneEntryBeforeTheNextMessageLeaveOnlyTheDelete(t *testing.T) {
	e := newPOE2E(t)
	_, first := e.ownerOf("first")
	gone := e.saveMemory(first, "Edited then deleted.")
	twice := e.saveMemory(first, "Edited twice.")
	id := e.idleThread("open thread")

	e.updateMemory(gone.EntryID, "Intermediate wording.")
	e.deleteMemory(gone.EntryID)
	e.updateMemory(twice.EntryID, "Second wording.")
	e.updateMemory(twice.EntryID, "Final wording.")

	got := e.sendAndWait(id, "question")
	if n := strings.Count(got, gone.EntryID); n != 1 {
		t.Errorf("the deleted entry is named %d times, want once: %q", n, got)
	}
	if n := strings.Count(got, twice.EntryID); n != 1 {
		t.Errorf("the edited entry is named %d times, want once: %q", n, got)
	}
	if !strings.Contains(got, "("+gone.EntryID+") was deleted") || strings.Contains(got, "("+gone.EntryID+") now reads") {
		t.Errorf("the deleted entry is not told as deleted alone: %q", got)
	}
	if !strings.Contains(got, "("+twice.EntryID+") now reads: Final wording.") || strings.Contains(got, "Second wording.") {
		t.Errorf("the edited entry is not told with its last text alone: %q", got)
	}
	if again := e.sendAndWait(id, "next"); again != "next" {
		t.Errorf("the note came again: %q", again)
	}
}

func TestTwoOpenThreadsEachGetEveryChangeOnceWhenEditsInterleaveWithTheirMessages(t *testing.T) {
	e := newPOE2E(t)
	_, first := e.ownerOf("first")
	one := e.saveMemory(first, "One.")
	two := e.saveMemory(first, "Two.")
	a, b := e.idleThread("thread a"), e.idleThread("thread b")

	e.updateMemory(one.EntryID, "One edited.")
	if got := e.sendAndWait(a, "a1"); !strings.Contains(got, "One edited.") || strings.Contains(got, two.EntryID) {
		t.Errorf("thread a's first note = %q", got)
	}
	e.updateMemory(two.EntryID, "Two edited.")
	if got := e.sendAndWait(a, "a2"); !strings.Contains(got, "Two edited.") || strings.Contains(got, one.EntryID) {
		t.Errorf("thread a's second note repeats or misses a change: %q", got)
	}
	got := e.sendAndWait(b, "b1")
	if strings.Count(got, one.EntryID) != 1 || strings.Count(got, two.EntryID) != 1 || !strings.Contains(got, "One edited.") || !strings.Contains(got, "Two edited.") {
		t.Errorf("thread b's note does not hold each change once: %q", got)
	}
	for _, id := range []string{a, b} {
		if got := e.sendAndWait(id, "plain"); got != "plain" {
			t.Errorf("thread %s was told again: %q", id, got)
		}
	}
}

func TestAThreadToldBeforeARestartIsToldNothingAgainAfterIt(t *testing.T) {
	e := newPOE2E(t)
	_, first := e.ownerOf("first")
	entry := e.saveMemory(first, "Before.")
	id := e.idleThread("open thread")
	e.updateMemory(entry.EntryID, "Told once.")
	if got := e.sendAndWait(id, "before the restart"); !strings.Contains(got, "Told once.") {
		t.Fatalf("the note before the restart = %q", got)
	}

	e.restart()
	if got := e.sendAndWait(id, "after the restart"); got != "after the restart" {
		t.Errorf("the thread was told again after the restart: %q", got)
	}
	e.updateMemory(entry.EntryID, "Changed after the restart.")
	got := e.sendAndWait(id, "once more")
	if !strings.Contains(got, "Changed after the restart.") || strings.Contains(got, "Told once.") || strings.Contains(got, "[Memory of this project") {
		t.Errorf("the note after the restart is not the delta: %q", got)
	}
}

func TestAnEditDuringARunningTurnReachesTheNextMessageOnce(t *testing.T) {
	e := newPOE2E(t)
	_, first := e.ownerOf("first")
	entry := e.saveMemory(first, "Before the turn.")
	id := e.idleThread("open thread")

	e.hold()
	before := len(e.received())
	if status, code := e.send(id, "running"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "running")
	eventuallyPO(t, "the message reaches the stand-in claude", func() string {
		if len(e.received()) == before+1 {
			return ""
		}
		return "the stand-in claude has not received the message"
	})
	e.updateMemory(entry.EntryID, "Edited mid-turn.")
	e.release()
	e.waitTurn(id, "idle")
	if got := e.received(); got[len(got)-1] != "running" {
		t.Errorf("the running message carried a note: %q", got[len(got)-1])
	}

	if got := e.sendAndWait(id, "after the turn"); !strings.Contains(got, "Edited mid-turn.") {
		t.Errorf("the edit made during the turn was lost: %q", got)
	}
	if got := e.sendAndWait(id, "plain"); got != "plain" {
		t.Errorf("the edit made during the turn was told twice: %q", got)
	}
}

func TestThePersonsEditAfterTheThreadsOwnSaveReachesThatThread(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("open thread")
	entry := e.saveMemory(owner, "The thread's wording.")
	e.updateMemory(entry.EntryID, "The person's wording.")
	if got := e.sendAndWait(id, "question"); !strings.Contains(got, "("+entry.EntryID+") now reads: The person's wording.") {
		t.Errorf("the thread was not told of the person's edit of its own entry: %q", got)
	}
}

func TestTheMemoryLimitIsExactly4000CharactersForThePersonAndTheTools(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	entry := e.saveMemory(owner, "Short.")

	atLimit := strings.Repeat("é", domain.ProductOwnerMaxMemory)
	if status, code := e.post("update_memory_entry", map[string]any{"entry_id": entry.EntryID, "text": "  " + atLimit + "\n"}, nil); status != 200 {
		t.Errorf("update with exactly 4,000 characters = %d %s, want 200", status, code)
	}
	if status, code := e.post("update_memory_entry", map[string]any{"entry_id": entry.EntryID, "text": atLimit + "é"}, nil); status != 400 || code != "INVALID_INPUT" {
		t.Errorf("update with 4,001 characters = %d %s, want 400 INVALID_INPUT", status, code)
	}
	if list := e.memoryList(e.project); len(list) != 1 || list[0].Text != atLimit {
		t.Errorf("the entries after the updates = %d, want one holding the 4,000 trimmed characters of the accepted update", len(list))
	}

	if out, refused := e.call(owner, "change_memory", map[string]any{"entry_id": entry.EntryID, "text": strings.Repeat("x", domain.ProductOwnerMaxMemory)}); refused {
		t.Errorf("change_memory with exactly 4,000 characters refused: %s", out)
	}
	if out, refused := e.call(owner, "change_memory", map[string]any{"entry_id": entry.EntryID, "text": strings.Repeat("x", domain.ProductOwnerMaxMemory+1)}); !refused || !strings.Contains(out, "INVALID_INPUT") {
		t.Errorf("change_memory with 4,001 characters = %q, want an INVALID_INPUT refusal", out)
	}
	if out, refused := e.call(owner, "save_memory", map[string]any{"text": strings.Repeat("x", domain.ProductOwnerMaxMemory+1)}); !refused || !strings.Contains(out, "INVALID_INPUT") {
		t.Errorf("save_memory with 4,001 characters = %q, want an INVALID_INPUT refusal", out)
	}
	e.saveMemory(owner, strings.Repeat("x", domain.ProductOwnerMaxMemory))
	if list := e.memoryList(e.project); len(list) != 2 {
		t.Errorf("entries = %d, want 2", len(list))
	}
}

func TestAnotherProjectsEntryIDIsNotFoundAndLeavesNoTraceAnywhere(t *testing.T) {
	e := newPOE2E(t)
	home, owner := e.ownerOf("home thread")
	secret := e.saveMemory(owner, "Home project only.")
	e.sendAndWait(home, "settle") // the home thread has seen the save
	_, foreignThread, foreign := e.otherProjectOwner()
	events := len(e.feed.of("product_owner_memory"))

	for name, args := range map[string]map[string]any{
		"change_memory": {"entry_id": secret.EntryID, "text": "hijack"},
		"drop_memory":   {"entry_id": secret.EntryID},
	} {
		out, refused := e.call(foreign, name, args)
		if !refused || !strings.Contains(out, "MEMORY_ENTRY_NOT_FOUND") || strings.Contains(out, secret.Text) {
			t.Errorf("%s from another project = %q, want MEMORY_ENTRY_NOT_FOUND without the entry's text", name, out)
		}
	}
	if out, _ := e.call(foreign, "list_memory", nil); strings.Contains(out, secret.EntryID) || !strings.Contains(out, `"count":0`) {
		t.Errorf("the other project's list = %s", out)
	}
	if notes := e.memoryNotes(foreignThread); len(notes) != 0 {
		t.Errorf("the other project's thread has memory notes: %v", notes)
	}
	if n := len(e.feed.of("product_owner_memory")); n != events {
		t.Errorf("memory events grew from %d to %d after refused calls", events, n)
	}
	if list := e.memoryList(e.project); len(list) != 1 || list[0].Text != secret.Text || list[0].ChangedAt != nil {
		t.Errorf("the home entry after the attempts = %+v", list)
	}
	if got := e.sendAndWait(home, "plain"); got != "plain" {
		t.Errorf("the home thread was told of a refused change: %q", got)
	}
}
