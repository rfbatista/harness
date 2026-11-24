package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"

	"operators-mcp/internal/domain"
)

// The product owner's memory (ADR 0111), at the wired server over the
// stand-in claude: the tools it calls through its session's MCP endpoint, the
// notes they leave in the thread, and what the store keeps.

// memoryEntry is an entry as the tools and the endpoint answer it.
type memoryEntry struct {
	EntryID     string  `json:"entry_id"`
	Text        string  `json:"text"`
	SavedAt     string  `json:"saved_at"`
	ChangedAt   *string `json:"changed_at"`
	ChangedBy   *string `json:"changed_by"`
	ThreadID    string  `json:"thread_id"`
	ThreadTitle string  `json:"thread_title"`
}

// ownerOf starts a thread, waits for its first answer and dials the tools its
// session is served.
func (e *poE2E) ownerOf(message string) (threadID string, owner *client.Client) {
	e.t.Helper()
	id, _ := e.startThread(message)
	got := e.waitTurn(id, "idle")
	return id, e.dialSession(got.Thread.SessionID)
}

// saveMemory calls the save tool and answers the entry it made.
func (e *poE2E) saveMemory(owner *client.Client, text string) memoryEntry {
	e.t.Helper()
	out, refused := e.call(owner, "save_memory", map[string]any{"text": text})
	if refused {
		e.t.Fatalf("save_memory refused: %s", out)
	}
	var entry memoryEntry
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		e.t.Fatalf("save_memory answered %q: %v", out, err)
	}
	return entry
}

// memoryNotes are the thread's memory_note items, oldest first.
func (e *poE2E) memoryNotes(threadID string) []map[string]any {
	e.t.Helper()
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if status, code := e.get("product_owner_thread?thread_id="+threadID, &page); status != 200 {
		e.t.Fatalf("GET thread = %d %s", status, code)
	}
	var notes []map[string]any
	for _, it := range page.Items {
		if it["kind"] == "memory_note" {
			notes = append(notes, it)
		}
	}
	return notes
}

func TestTheProductOwnerSavesAnEntryAndTheThreadAndTheFeedSayIt(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("we decided to ship on fridays")

	entry := e.saveMemory(owner, "Ship on Fridays.")
	if entry.EntryID == "" || entry.Text != "Ship on Fridays." || entry.ThreadID != id || entry.ChangedAt != nil || entry.ChangedBy != nil {
		t.Fatalf("saved entry = %+v", entry)
	}
	if entry.ThreadTitle != e.thread(id).Thread.Title {
		t.Errorf("thread_title = %q, want %q", entry.ThreadTitle, e.thread(id).Thread.Title)
	}

	notes := e.memoryNotes(id)
	if len(notes) != 1 || notes[0]["op"] != "saved" || notes[0]["entry_id"] != entry.EntryID || notes[0]["text"] != "Ship on Fridays." {
		t.Fatalf("memory notes = %v", notes)
	}
	eventuallyPO(t, "the feed to carry the memory event", func() string {
		evs := e.feed.of("product_owner_memory")
		if len(evs) != 1 || evs[0]["op"] != "saved" || evs[0]["entry_id"] != entry.EntryID || evs[0]["project_id"] != e.project {
			return "events = " + strings.Join(e.feed.changesOf("product_owner_memory"), ",")
		}
		return ""
	})

	// The note reaches the page stream as a thread item too.
	eventuallyPO(t, "the thread item event of the note", func() string {
		for _, ev := range e.feed.of("product_owner_thread_item") {
			if item, _ := ev["item"].(map[string]any); item["kind"] == "memory_note" {
				return ""
			}
		}
		return "no memory_note on the feed"
	})
}

func TestTheProductOwnerChangesAndDropsAnEntry(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("hello")
	first := e.saveMemory(owner, "Ship on Fridays.")

	out, refused := e.call(owner, "change_memory", map[string]any{"entry_id": first.EntryID, "text": "Ship on Thursdays."})
	if refused {
		t.Fatalf("change_memory refused: %s", out)
	}
	var changed memoryEntry
	if err := json.Unmarshal([]byte(out), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.EntryID != first.EntryID || changed.Text != "Ship on Thursdays." || changed.SavedAt != first.SavedAt ||
		changed.ChangedAt == nil || changed.ChangedBy == nil || *changed.ChangedBy != "product_owner" {
		t.Fatalf("changed entry = %+v (saved as %+v)", changed, first)
	}

	if out, refused := e.call(owner, "drop_memory", map[string]any{"entry_id": first.EntryID}); refused {
		t.Fatalf("drop_memory refused: %s", out)
	}
	var ops []string
	for _, n := range e.memoryNotes(id) {
		ops = append(ops, n["op"].(string))
	}
	if !slices.Equal(ops, []string{"saved", "changed", "dropped"}) {
		t.Errorf("note ops = %v", ops)
	}
	eventuallyPO(t, "three memory events", func() string {
		if n := len(e.feed.of("product_owner_memory")); n != 3 {
			return "events = " + strings.Join(e.feed.changesOf("product_owner_memory"), ",")
		}
		return ""
	})
	if out, _ := e.call(owner, "list_memory", nil); strings.Contains(out, "Ship on") {
		t.Errorf("a dropped entry is listed: %s", out)
	}
}

func TestTheListToolAnswersTheProjectsEntriesNewestFirstAcrossThreads(t *testing.T) {
	e := newPOE2E(t)
	_, first := e.ownerOf("one")
	e.saveMemory(first, "Older fact.")
	_, second := e.ownerOf("two")
	e.saveMemory(second, "Newer fact.")

	for name, c := range map[string]*client.Client{"first thread": first, "second thread": second} {
		out, refused := e.call(c, "list_memory", nil)
		if refused {
			t.Fatalf("%s: list_memory refused: %s", name, out)
		}
		var listed struct {
			Entries []memoryEntry `json:"entries"`
		}
		if err := json.Unmarshal([]byte(out), &listed); err != nil {
			t.Fatal(err)
		}
		if len(listed.Entries) != 2 || listed.Entries[0].Text != "Newer fact." || listed.Entries[1].Text != "Older fact." {
			t.Errorf("%s: entries = %+v", name, listed.Entries)
		}
	}
}

func TestTheMemoryToolsRefuseBadInputAndStoreNothing(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("hello")
	kept := e.saveMemory(owner, "Kept.")

	for name, c := range map[string]struct {
		tool string
		args map[string]any
	}{
		"empty text":      {"save_memory", map[string]any{"text": "  "}},
		"overlong text":   {"save_memory", map[string]any{"text": strings.Repeat("é", domain.ProductOwnerMaxMemory+1)}},
		"change to empty": {"change_memory", map[string]any{"entry_id": kept.EntryID, "text": ""}},
		"change overlong": {"change_memory", map[string]any{"entry_id": kept.EntryID, "text": strings.Repeat("x", domain.ProductOwnerMaxMemory+1)}},
		"change unknown":  {"change_memory", map[string]any{"entry_id": "nope", "text": "x"}},
		"drop unknown":    {"drop_memory", map[string]any{"entry_id": "nope"}},
		"drop without id": {"drop_memory", map[string]any{}},
	} {
		out, refused := e.call(owner, c.tool, c.args)
		if !refused || out == "" {
			t.Errorf("%s: answered %q, want a refusal", name, out)
		}
	}
	// The limit counts characters, not bytes: the full length is accepted.
	e.saveMemory(owner, strings.Repeat("é", domain.ProductOwnerMaxMemory))

	entries, err := e.store.MemoryEntries(e.ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || len(e.memoryNotes(id)) != 2 {
		t.Errorf("entries = %d, notes = %d, want 2 and 2", len(entries), len(e.memoryNotes(id)))
	}
	if entries[1].Text != "Kept." {
		t.Errorf("a refused change altered the entry: %q", entries[1].Text)
	}
}

func TestAnotherProjectsProductOwnerNeverSeesOrReachesAnEntry(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	secret := e.saveMemory(owner, "Only for the harness project.")

	other, err := e.cat.Projects.CreateProject(e.ctx, "other", e.t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cat.Projects.CreateRepository(e.ctx, other.ID, "other", "", "https://example.com/other.git", e.t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var started struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": other.ID, "message": "hi"}, &started); status != 200 {
		t.Fatalf("start in other project = %d %s", status, code)
	}
	foreign := e.dialSession(e.waitTurn(started.ThreadID, "idle").Thread.SessionID)

	if out, _ := e.call(foreign, "list_memory", nil); strings.Contains(out, secret.Text) {
		t.Errorf("the other project lists the entry: %s", out)
	}
	if out, refused := e.call(foreign, "change_memory", map[string]any{"entry_id": secret.EntryID, "text": "hijack"}); !refused {
		t.Errorf("the other project changed the entry: %s", out)
	}
	if out, refused := e.call(foreign, "drop_memory", map[string]any{"entry_id": secret.EntryID}); !refused {
		t.Errorf("the other project dropped the entry: %s", out)
	}
	entries, err := e.store.MemoryEntries(e.ctx, e.project)
	if err != nil || len(entries) != 1 || entries[0].Text != secret.Text {
		t.Errorf("entries after the attempts = %+v, %v", entries, err)
	}
}

func TestMemoryIsUnchangedAfterARestartAndTheToolsAreServedAfterIt(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("hello")
	saved := e.saveMemory(owner, "Survives a restart.")

	e.restart()
	entries, err := e.store.MemoryEntries(e.ctx, e.project)
	if err != nil || len(entries) != 1 || entries[0].ID != saved.EntryID || entries[0].Text != saved.Text || entries[0].ThreadID != id {
		t.Fatalf("entries after the restart = %+v, %v", entries, err)
	}
	if n := len(e.memoryNotes(id)); n != 1 {
		t.Errorf("notes after the restart = %d", n)
	}
}

func TestTheProductOwnerIsServedTheMemoryToolsAndTheBriefTellsHowToUseThem(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	got := e.toolNames(owner)
	for _, name := range []string{"list_memory", "save_memory", "change_memory", "drop_memory"} {
		if !slices.Contains(got, name) {
			t.Errorf("the product owner is not served %s: %v", name, got)
		}
	}
	l := e.launches()
	if len(l) != 1 {
		t.Fatalf("launches = %q", l)
	}
	for _, want := range []string{"save_memory", "memory", "secret"} {
		if !strings.Contains(l[0], want) {
			t.Errorf("the opening brief lacks %q", want)
		}
	}
}

// memoryList reads the project's entries through the endpoint.
func (e *poE2E) memoryList(project string) []memoryEntry {
	e.t.Helper()
	var out struct {
		Entries []memoryEntry `json:"entries"`
	}
	if status, code := e.get("product_owner_memory?project_id="+project, &out); status != 200 {
		e.t.Fatalf("GET memory = %d %s", status, code)
	}
	return out.Entries
}

func TestThePersonReadsTheEntriesNewestFirstWithTheirThreadAndTheyKeepTheirOrderAcrossARestart(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("what did we agree")
	e.saveMemory(owner, "First.")
	e.saveMemory(owner, "Second.")

	list := e.memoryList(e.project)
	if len(list) != 2 || list[0].Text != "Second." || list[1].Text != "First." {
		t.Fatalf("entries = %+v", list)
	}
	if list[0].ThreadID != id || list[0].ThreadTitle != e.thread(id).Thread.Title || list[0].ChangedAt != nil || list[0].ChangedBy != nil || list[0].SavedAt == "" {
		t.Errorf("entry = %+v", list[0])
	}
	e.restart()
	if after := e.memoryList(e.project); len(after) != 2 || after[0] != list[0] && after[0].EntryID != list[0].EntryID {
		t.Errorf("entries after the restart = %+v", after)
	}
	if status, code := e.get("product_owner_memory?project_id=nope", nil); status != 404 {
		t.Errorf("an unknown project = %d %s, want 404", status, code)
	}
}

func TestThePersonEditsAnEntryAndTheProductOwnerSeesTheNewText(t *testing.T) {
	e := newPOE2E(t)
	id, owner := e.ownerOf("hello")
	saved := e.saveMemory(owner, "Ship on Fridays.")

	var edited memoryEntry
	if status, code := e.post("update_memory_entry", map[string]any{"entry_id": saved.EntryID, "text": "  Ship on Thursdays.  "}, &edited); status != 200 {
		t.Fatalf("update = %d %s", status, code)
	}
	if edited.Text != "Ship on Thursdays." || edited.ChangedBy == nil || *edited.ChangedBy != "person" || edited.ChangedAt == nil || edited.SavedAt != saved.SavedAt || edited.ThreadID != id {
		t.Fatalf("edited = %+v", edited)
	}
	if list := e.memoryList(e.project); len(list) != 1 || list[0].Text != "Ship on Thursdays." {
		t.Errorf("list = %+v", list)
	}
	if out, _ := e.call(owner, "list_memory", nil); !strings.Contains(out, "Ship on Thursdays.") {
		t.Errorf("the product owner reads %s", out)
	}
	// The person's edit leaves no item in the thread: only the product owner's calls do.
	if n := len(e.memoryNotes(id)); n != 1 {
		t.Errorf("notes = %d, want only the one the save left", n)
	}
	eventuallyPO(t, "one changed event", func() string {
		evs := e.feed.of("product_owner_memory")
		if len(evs) != 2 || evs[1]["op"] != "changed" || evs[1]["entry_id"] != saved.EntryID {
			return "events = " + strings.Join(e.feed.changesOf("product_owner_memory"), ",")
		}
		return ""
	})
}

func TestThePersonDeletesAnEntry(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	saved := e.saveMemory(owner, "Gone soon.")

	var out map[string]any
	if status, code := e.post("delete_memory_entry", map[string]any{"entry_id": saved.EntryID}, &out); status != 200 || len(out) != 0 {
		t.Fatalf("delete = %d %s %v", status, code, out)
	}
	if list := e.memoryList(e.project); len(list) != 0 {
		t.Errorf("list = %+v", list)
	}
	if text, _ := e.call(owner, "list_memory", nil); strings.Contains(text, "Gone soon.") {
		t.Errorf("the product owner still lists it: %s", text)
	}
	eventuallyPO(t, "a dropped event", func() string {
		evs := e.feed.of("product_owner_memory")
		if len(evs) != 2 || evs[1]["op"] != "dropped" {
			return "events = " + strings.Join(e.feed.changesOf("product_owner_memory"), ",")
		}
		return ""
	})
}

func TestTheMemoryEndpointsRefuseWhatTheContractSays(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	saved := e.saveMemory(owner, "Kept.")

	for name, c := range map[string]struct {
		path       string
		body       map[string]any
		wantStatus int
		wantCode   string
	}{
		"update unknown":  {"update_memory_entry", map[string]any{"entry_id": "nope", "text": "x"}, 404, "MEMORY_ENTRY_NOT_FOUND"},
		"delete unknown":  {"delete_memory_entry", map[string]any{"entry_id": "nope"}, 404, "MEMORY_ENTRY_NOT_FOUND"},
		"update empty":    {"update_memory_entry", map[string]any{"entry_id": saved.EntryID, "text": " "}, 400, "INVALID_INPUT"},
		"update too long": {"update_memory_entry", map[string]any{"entry_id": saved.EntryID, "text": strings.Repeat("x", 4001)}, 400, "INVALID_INPUT"},
	} {
		if status, code := e.post(c.path, c.body, nil); status != c.wantStatus || code != c.wantCode {
			t.Errorf("%s = %d %s, want %d %s", name, status, code, c.wantStatus, c.wantCode)
		}
	}
	if list := e.memoryList(e.project); len(list) != 1 || list[0].Text != "Kept." || list[0].ChangedAt != nil {
		t.Errorf("a refusal changed the entry: %+v", list)
	}
	if n := len(e.feed.of("product_owner_memory")); n != 1 {
		t.Errorf("events = %d, a refusal announced one", n)
	}
}

func TestAnotherProjectsListHoldsNoEntryOfThisOne(t *testing.T) {
	e := newPOE2E(t)
	_, owner := e.ownerOf("hello")
	e.saveMemory(owner, "Private.")
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", e.t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if list := e.memoryList(other.ID); len(list) != 0 {
		t.Errorf("the other project lists %+v", list)
	}
}
