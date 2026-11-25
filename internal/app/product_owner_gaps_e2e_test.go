//go:build !windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// gatedSandbox is the sandbox driver the stand-in runs under, holding each Wrap
// while the file "slowstart" exists beside the stand-in. Claude is wrapped as
// the process starts, so the hold keeps a thread in its resuming turn.
type gatedSandbox struct {
	ports.SandboxDriver
	gate string
}

func (g gatedSandbox) Wrap(p ports.SandboxPolicy, c ports.SandboxCommand) (ports.SandboxCommand, error) {
	for {
		if _, err := os.Stat(g.gate); err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return g.SandboxDriver.Wrap(p, c)
}

// gateStart installs the gate on the running server's sandbox driver; a
// restart installs a fresh driver, so call it again afterwards.
func (e *poE2E) gateStart() {
	e.t.Helper()
	if e.orch.Sandbox == nil {
		e.t.Skip("the server has no sandbox to gate")
	}
	e.orch.Sandbox.Driver = gatedSandbox{SandboxDriver: e.sandbox, gate: filepath.Join(e.fakeDir, "slowstart")}
}

func (e *poE2E) slow(on bool) {
	e.t.Helper()
	p := filepath.Join(e.fakeDir, "slowstart")
	if on {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			e.t.Fatal(err)
		}
		return
	}
	_ = os.Remove(p)
}

func (e *poE2E) page(id, before string) poThread {
	e.t.Helper()
	path := "product_owner_thread?thread_id=" + id
	if before != "" {
		path += "&before=" + before
	}
	var out poThread
	if status, code := e.get(path, &out); status != 200 {
		e.t.Fatalf("GET %s = %d %s", path, status, code)
	}
	return out
}

func TestPagingEdgesAtExactlyOneHundredItemsOneMoreAndBeforeTheFirstItem(t *testing.T) {
	e := newPOE2E(t)

	hundred := e.seedThread("hundred", time.Now(), 100)
	p := e.page(hundred, "")
	if len(p.Items) != 100 || p.HasEarlier || p.Items[0].Text != "message 1" || p.Items[99].Text != "message 100" {
		t.Errorf("100 items: got %d, has_earlier %v; want all 100 and no earlier", len(p.Items), p.HasEarlier)
	}
	// Nothing lies before the first item.
	if q := e.page(hundred, p.Items[0].ItemID); len(q.Items) != 0 || q.HasEarlier {
		t.Errorf("before the first item: %d items, has_earlier %v; want an empty page and no earlier", len(q.Items), q.HasEarlier)
	}
	// Before the second item is the first alone.
	if q := e.page(hundred, p.Items[1].ItemID); len(q.Items) != 1 || q.Items[0].Text != "message 1" || q.HasEarlier {
		t.Errorf("before the second item: %+v has_earlier %v", q.Items, q.HasEarlier)
	}

	more := e.seedThread("hundred-one", time.Now(), 101)
	p = e.page(more, "")
	if len(p.Items) != 100 || !p.HasEarlier || p.Items[0].Text != "message 2" {
		t.Fatalf("101 items: got %d, has_earlier %v, first %q; want the newest 100 and an earlier one", len(p.Items), p.HasEarlier, p.Items[0].Text)
	}
	q := e.page(more, p.Items[0].ItemID)
	if len(q.Items) != 1 || q.Items[0].Text != "message 1" || q.HasEarlier {
		t.Errorf("the page before: %+v has_earlier %v; want message 1 alone", q.Items, q.HasEarlier)
	}

	// An item of another thread is no place in this one.
	other := e.page(hundred, "")
	if status, code := e.get("product_owner_thread?thread_id="+more+"&before="+other.Items[50].ItemID, nil); status != 400 && status != 404 {
		t.Errorf("a before of another thread's item = %d %s, want a refusal", status, code)
	}

	// An empty thread reads as an empty list, never null.
	empty := e.seedThread("empty", time.Now(), 0)
	if p := e.page(empty, ""); p.Items == nil || len(p.Items) != 0 || p.HasEarlier {
		t.Errorf("empty thread = %+v", p)
	}
}

func TestARenameTakesTwoHundredCharactersAndRefusesTwoHundredAndOne(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("q")
	e.waitTurn(id, "idle")
	rename := func(title string) (int, string) {
		return e.post("rename_product_owner_thread", map[string]any{"thread_id": id, "title": title}, nil)
	}
	two := strings.Repeat("x", 200)
	if status, code := rename(two); status != 200 {
		t.Fatalf("200 characters = %d %s, want 200", status, code)
	}
	if got := e.thread(id).Thread.Title; got != two {
		t.Errorf("title = %d characters, want the 200 sent", len([]rune(got)))
	}
	// Characters, not bytes.
	wide := strings.Repeat("é", 200)
	if status, code := rename(wide); status != 200 {
		t.Errorf("200 two-byte characters = %d %s, want 200", status, code)
	}
	if status, code := rename(strings.Repeat("é", 201)); status != 400 || code != "INVALID_INPUT" {
		t.Errorf("201 characters = %d %s, want 400 INVALID_INPUT", status, code)
	}
	if got := e.thread(id).Thread.Title; got != wide {
		t.Errorf("a refused rename changed the title to %d characters", len([]rune(got)))
	}
}

func TestAMessageOfExactlyTwentyThousandCharactersAndFiveAttachmentsAreWithinTheLimits(t *testing.T) {
	e := newPOE2E(t)
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": e.project, "message": strings.Repeat("x", 20000)}, nil); status != 200 {
		t.Errorf("20,000 characters = %d %s, want 200", status, code)
	}
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "prompt": strings.Repeat("p", 20000)}, nil); status != 200 {
		t.Errorf("a prompt of 20,000 characters = %d %s, want 200", status, code)
	}
	// Five attachments pass the count; the unknown id is what refuses them.
	status, code := e.post("start_product_owner_thread", map[string]any{"project_id": e.project, "message": "hi", "attachment_ids": []string{"a", "b", "c", "d", "e"}}, nil)
	if status != 400 || code != "INVALID_INPUT" {
		t.Errorf("five unknown attachments = %d %s, want 400 INVALID_INPUT", status, code)
	}
}

func TestAMessageWhileTheThreadReadsResumingIsRefusedAsBusyAndStoresNothing(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})

	e.gateStart()
	e.slow(true)
	done := make(chan [2]any, 1)
	go func() {
		status, code := e.send(id, "second")
		done <- [2]any{status, code}
	}()
	eventuallyPO(t, "the thread to read resuming", func() string {
		if got := e.thread(id).Thread.Turn; got != "resuming" {
			return "turn is " + got
		}
		return ""
	})
	// The thread's lock is held while it wakes: the second message waits for
	// the wake-up and is then refused, not stored and not sent.
	third := make(chan [2]any, 1)
	go func() {
		status, code := e.send(id, "third")
		third <- [2]any{status, code}
	}()
	time.Sleep(300 * time.Millisecond)
	if n := countItems(e.thread(id).Items, "message", "third"); n != 0 {
		t.Errorf("a message sent while resuming was stored before the wake-up ended")
	}
	e.slow(false)
	if r := <-third; r[0] != 409 || r[1] != "THREAD_BUSY" {
		t.Errorf("a message while resuming = %v, want 409 THREAD_BUSY", r)
	}
	if r := <-done; r[0] != 200 {
		t.Errorf("the message that woke the thread = %v", r)
	}
	if n := countItems(e.thread(id).Items, "message", "third"); n != 0 {
		t.Errorf("the refused message was stored")
	}
	got := e.waitTurn(id, "idle")
	if countItems(got.Items, "message", "second") != 2 { // the person's and the answer
		t.Errorf("items = %+v, want the second message and its answer", got.Items)
	}
}

// A restart cannot be taken while a request is held open (the test server waits
// for it), so the state a stop in the middle of a wake-up leaves is stored
// directly: the person's message kept, the turn reading resuming, no process.
func TestARestartWhileAThreadReadsResumingLosesNoMessageAndEndsIdle(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})
	e.feed.stop()
	e.srv.Close()
	if err := e.app.Stop(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.boot()
	stored, err := e.store.Thread(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
		Author: domain.ProductOwnerAuthorPerson, Text: "second"}); err != nil {
		t.Fatal(err)
	}
	stored.Turn = domain.ProductOwnerTurnResuming
	if err := e.store.SaveThread(e.ctx, *stored); err != nil {
		t.Fatal(err)
	}
	e.restart()

	got := e.waitTurn(id, "idle")
	persons := 0
	for _, it := range got.Items {
		if it.Kind == "message" && it.Author == "person" && it.Text == "second" {
			persons++
		}
		if it.Streaming != nil && *it.Streaming {
			t.Errorf("an item is still streaming: %+v", it)
		}
	}
	if persons != 1 {
		t.Errorf("%d copies of the person's message, want 1: %+v", persons, got.Items)
	}
	if status, code := e.send(id, "after"); status != 200 {
		t.Errorf("a message after the restart = %d %s, want 200", status, code)
	}
	e.waitTurn(id, "idle")
}

func TestTwoProjectsKeepTheirThreadsListsFeedsAndSwitchesApart(t *testing.T) {
	e := newPOE2E(t)
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherFeed := followPOFeed(t, e.srv.URL, other.ID)

	idA, _ := e.startThread("about A")
	e.waitTurn(idA, "idle")
	var started struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": other.ID, "message": "about B"}, &started); status != 200 {
		t.Fatalf("start in the other project = %d %s", status, code)
	}
	idB := started.ThreadID
	e.waitTurn(idB, "idle")

	if got := e.thread(idB).Thread.ProjectID; got != other.ID {
		t.Errorf("thread B belongs to %s, want %s", got, other.ID)
	}
	list := func(project string) []string {
		var out struct {
			Threads []struct {
				ThreadID string `json:"thread_id"`
			} `json:"threads"`
		}
		e.get("product_owner_threads?project_id="+project, &out)
		var ids []string
		for _, th := range out.Threads {
			ids = append(ids, th.ThreadID)
		}
		return ids
	}
	if got := list(e.project); len(got) != 1 || got[0] != idA {
		t.Errorf("project A lists %v, want only %s", got, idA)
	}
	if got := list(other.ID); len(got) != 1 || got[0] != idB {
		t.Errorf("project B lists %v, want only %s", got, idB)
	}

	eventuallyPO(t, "each feed to carry its own thread", func() string {
		if len(e.feed.of("product_owner_thread_item")) == 0 || len(otherFeed.of("product_owner_thread_item")) == 0 {
			return "a feed has no thread item yet"
		}
		return ""
	})
	time.Sleep(100 * time.Millisecond)
	for _, key := range []string{"product_owner_thread_item", "product_owner_delta", "product_owner_thread"} {
		for _, c := range e.feed.of(key) {
			if c["project_id"] != e.project || c["thread_id"] == idB || fmt.Sprint(c["thread"]) != "<nil>" && strings.Contains(fmt.Sprint(c["thread"]), idB) {
				t.Errorf("project A's feed carried %s of project B: %v", key, c)
			}
		}
		for _, c := range otherFeed.of(key) {
			if c["project_id"] != other.ID || c["thread_id"] == idA || fmt.Sprint(c["thread"]) != "<nil>" && strings.Contains(fmt.Sprint(c["thread"]), idA) {
				t.Errorf("project B's feed carried %s of project A: %v", key, c)
			}
		}
	}

	// B's switch is B's: a message to A's thread still goes through.
	if status, _ := e.post("update_product_owner", map[string]any{"project_id": other.ID, "enabled": false}, nil); status != 200 {
		t.Fatal("switch off failed")
	}
	if status, code := e.send(idB, "more"); status != 409 || code != "PRODUCT_OWNER_OFF" {
		t.Errorf("send to B = %d %s, want 409 PRODUCT_OWNER_OFF", status, code)
	}
	if status, code := e.send(idA, "more"); status != 200 {
		t.Errorf("send to A = %d %s, want 200", status, code)
	}
	e.waitTurn(idA, "idle")
	if !e.settings().Enabled {
		t.Error("switching B off switched A off")
	}
}

func TestAnIdleEndLeavesTheThreadIdleWithNoNoticeAndARestartDoesNotWakeIt(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})
	before := e.thread(id)
	if before.Thread.Turn != "idle" || countItems(before.Items, "notice", "") != 0 || countItems(before.Items, "failure", "") != 0 {
		t.Errorf("after the idle end: %+v", before)
	}
	e.restart()
	time.Sleep(300 * time.Millisecond)
	after := e.thread(id)
	if len(after.Items) != len(before.Items) || after.Thread.Turn != "idle" || len(e.launches()) != 1 {
		t.Errorf("a restart woke an ended idle thread: %d items (was %d), turn %s, %d launches", len(after.Items), len(before.Items), after.Thread.Turn, len(e.launches()))
	}
	if status, code := e.send(id, "second"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	got := e.waitTurn(id, "idle")
	if countItems(got.Items, "message", "second") != 2 || len(e.launches()) != 2 {
		t.Errorf("after the send: %+v, %d launches", got.Items, len(e.launches()))
	}
}

func TestTheProductOwnerIsRefusedAnotherSessionsTranscriptAndWorktreeAndAnotherProjectsTasks(t *testing.T) {
	e := newPOE2E(t)
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.plan.CreateTicket(e.ctx, other.ID, "Secret plan of another project", "", domain.TicketStatusBacklog, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sess.Create(&domain.Session{ID: "other-session", ProjectID: e.project, Task: "implement", WorkingDir: "/secret/worktrees/x", WorkspaceID: "ws-secret",
		Branch: "agent/x", Status: domain.SessionRunning, Mode: domain.SessionModeImplement}); err != nil {
		t.Fatal(err)
	}
	id, _ := e.startThread("hello")
	owner := e.dialSession(e.thread(id).Thread.SessionID)

	for _, name := range []string{"get_session_transcript", "read_session_transcript", "get_session", "get_session_worktree", "read_session_worktree", "list_task_sessions", "get_transcript"} {
		if text, refused := e.call(owner, name, map[string]any{"session_id": "other-session"}); !refused {
			t.Errorf("%s was answered: %s", name, text)
		}
	}
	// What it may read of that session shows no path, and asking by id adds nothing.
	text, _ := e.call(owner, "list_project_sessions", map[string]any{"session_id": "other-session", "include_ended": true})
	for _, secret := range []string{"/secret", "ws-secret", "working_dir", "transcript", "worktree"} {
		if strings.Contains(text, secret) {
			t.Errorf("list_project_sessions shows %q: %s", secret, text)
		}
	}
	// A task of another project is not found from this project's thread.
	if text, refused := e.call(owner, "get_project_task", map[string]any{"task_id": foreign.ID}); !refused && strings.Contains(text, "Secret plan") {
		t.Errorf("another project's task was read: %s", text)
	}
	if text, _ := e.call(owner, "list_project_tasks", nil); strings.Contains(text, "Secret plan") {
		t.Errorf("another project's task was listed: %s", text)
	}
}
