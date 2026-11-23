package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func changedOf(e domain.ContextEntry, id string) *domain.ContextPieceChange {
	p, _ := e.Piece(id)
	return p.Changed
}

func TestSessionContextChain_EachResumeIsComparedWithTheEntryJustBeforeIt(t *testing.T) {
	svc := contextService(t)
	t0 := time.Unix(1000, 0)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, t0,
		fileP("CLAUDE.md", "1", "v1"), textP("task-brief", "brief A"), textP("services", "S")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, t0.Add(time.Hour),
		fileP("CLAUDE.md", "2", "v2"), textP("task-brief", "brief B"), textP("services", "S")))
	// e3 is the same as e2: against the start it would differ, against e2 it does not.
	storeEntry(t, svc, entryWith("e3", domain.ContextEntryResume, t0.Add(2*time.Hour),
		fileP("CLAUDE.md", "2", "v2"), textP("task-brief", "brief B"), textP("services", "S")))
	// e4 goes back to the start's text: a change against e3, not "unchanged since the start".
	storeEntry(t, svc, entryWith("e4", domain.ContextEntryResume, t0.Add(3*time.Hour),
		fileP("CLAUDE.md", "1", "v1"), textP("task-brief", "brief A"), textP("services", "S")))

	v := contextOf(t, svc, "s1")
	if len(v.Entries) != 4 {
		t.Fatalf("entries = %d", len(v.Entries))
	}
	for _, id := range []string{"CLAUDE.md", "task-brief", "services"} {
		if c := changedOf(v.Entries[0], id); c != nil {
			t.Errorf("start: %s marked %+v", id, c)
		}
	}
	if c := changedOf(v.Entries[1], "CLAUDE.md"); c == nil || c.FromVersion != "1" || c.ToVersion != "2" {
		t.Errorf("e2 CLAUDE.md = %+v", c)
	}
	if c := changedOf(v.Entries[1], "task-brief"); c == nil {
		t.Error("e2 task-brief not marked")
	}
	if c := changedOf(v.Entries[1], "services"); c != nil {
		t.Errorf("e2 services marked %+v", c)
	}
	for _, id := range []string{"CLAUDE.md", "task-brief", "services"} {
		if c := changedOf(v.Entries[2], id); c != nil {
			t.Errorf("e3 (the same as e2): %s marked %+v", id, c)
		}
	}
	if c := changedOf(v.Entries[3], "CLAUDE.md"); c == nil || c.FromVersion != "2" || c.ToVersion != "1" {
		t.Errorf("e4 CLAUDE.md = %+v, want 2 → 1", c)
	}
	b3, b4 := changedOf(v.Entries[3], "task-brief"), changedOf(v.Entries[1], "task-brief")
	if b3 == nil || b4 == nil || b3.FromVersion != b4.ToVersion || b3.ToVersion != b4.FromVersion {
		t.Errorf("hashes must be of the text: e4 %+v is the reverse of e2 %+v", b3, b4)
	}
}

func TestSessionContextChain_AMarkIsFixedWhenItsEntryIsStoredAndNeverMovesWhenALaterOneArrives(t *testing.T) {
	svc := contextService(t)
	t0 := time.Unix(1000, 0)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, t0, fileP("CLAUDE.md", "1", "a")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, t0.Add(time.Hour), fileP("CLAUDE.md", "2", "b")))
	before := changedOf(contextOf(t, svc, "s1").Entries[1], "CLAUDE.md")
	storeEntry(t, svc, entryWith("e3", domain.ContextEntryResume, t0.Add(2*time.Hour), fileP("CLAUDE.md", "3", "c")))
	after := changedOf(contextOf(t, svc, "s1").Entries[1], "CLAUDE.md")
	if before == nil || after == nil || *before != *after {
		t.Errorf("e2's mark moved from %+v to %+v", before, after)
	}
}

func TestSessionContextChain_AVersionedPieceWhoseTextChangedButVersionDidNotIsNotMarked(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Unix(1, 0), fileP("CLAUDE.md", "3", "one")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Unix(2, 0), fileP("CLAUDE.md", "3", "two")))
	if c := changedOf(contextOf(t, svc, "s1").Entries[1], "CLAUDE.md"); c != nil {
		t.Errorf("a version is the file's identity: same version marked %+v", c)
	}
}

func TestSessionContextChain_AFileLosingOrGainingItsVersionIsMarked(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Unix(1, 0), fileP("A.md", "", "same"), fileP("B.md", "2", "same")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Unix(2, 0), fileP("A.md", "1", "same"), fileP("B.md", "", "same")))
	e := contextOf(t, svc, "s1").Entries[1]
	if c := changedOf(e, "A.md"); c == nil || c.FromVersion != "" || c.ToVersion != "1" {
		t.Errorf("A.md = %+v", c)
	}
	if c := changedOf(e, "B.md"); c == nil || c.FromVersion != "2" || c.ToVersion != "" {
		t.Errorf("B.md = %+v", c)
	}
}

func TestSessionContextChain_APieceMissingFromOneEntryHasNoPreviousInTheNext(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Unix(1, 0), fileP("X.md", "1", "x")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Unix(2, 0)))
	storeEntry(t, svc, entryWith("e3", domain.ContextEntryResume, time.Unix(3, 0), fileP("X.md", "9", "x9")))
	if c := changedOf(contextOf(t, svc, "s1").Entries[2], "X.md"); c != nil {
		t.Errorf("e3 is compared with e2, which had no X.md: %+v", c)
	}
}

func TestSessionContextChain_AStartEntryIsNeverMarkedEvenAfterOthers(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Unix(1, 0), fileP("X.md", "1", "x")))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryStart, time.Unix(2, 0), fileP("X.md", "2", "y")))
	if c := changedOf(contextOf(t, svc, "s1").Entries[1], "X.md"); c != nil {
		t.Errorf("only a resume entry is compared: %+v", c)
	}
}

func resumeAfterEnd(t *testing.T, svc *Service, id string) {
	t.Helper()
	if _, err := svc.EndInteractive(context.Background(), id, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), resumeOf(id)); err != nil {
		t.Fatal(err)
	}
}

func TestSessionContextChain_AStartAndTwoRealResumesMarkOnlyWhatChanged(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})

	resumeAfterEnd(t, svc, sess.ID) // nothing changed
	svc.tickets.(fakeTickets)["tk1"].Title = "Ship the other thing"
	resumeAfterEnd(t, svc, sess.ID) // the brief changed

	v := contextOf(t, svc, sess.ID)
	if len(v.Entries) != 3 {
		t.Fatalf("entries = %d, want start + 2 resumes", len(v.Entries))
	}
	kinds := []string{v.Entries[0].Kind, v.Entries[1].Kind, v.Entries[2].Kind}
	if kinds[0] != domain.ContextEntryStart || kinds[1] != domain.ContextEntryResume || kinds[2] != domain.ContextEntryResume {
		t.Errorf("kinds = %v", kinds)
	}
	ids := map[string]bool{}
	for _, e := range v.Entries {
		if ids[e.ID] {
			t.Errorf("entry id %s repeated", e.ID)
		}
		ids[e.ID] = true
		if _, ok := e.Piece("task-brief"); !ok {
			t.Errorf("entry %s has no task-brief: piece ids must stay stable across entries", e.ID)
		}
	}
	for _, p := range v.Entries[1].Instructions {
		if p.Changed != nil {
			t.Errorf("a resume that changed nothing marks %s: %+v", p.ID, p.Changed)
		}
	}
	if c := changedOf(v.Entries[2], "task-brief"); c == nil || c.FromVersion == c.ToVersion {
		t.Errorf("the second resume's brief = %+v, want it marked changed against the first resume", c)
	}
	// Entries are never rebuilt: the start still has the title it started with.
	if p, _ := v.Entries[0].Piece("task-brief"); !strings.Contains(p.Text, "Ship the thing") || strings.Contains(p.Text, "other thing") {
		t.Errorf("the start entry was rewritten from current settings")
	}
	if p, _ := v.Entries[2].Piece("task-brief"); !strings.Contains(p.Text, "Ship the other thing") {
		t.Errorf("the second resume does not hold the brief it resumed with")
	}
	added := 0
	for _, c := range v.Changes {
		if c.Kind == domain.ContextChangeEntryAdded {
			added++
		}
	}
	if added != 3 {
		t.Errorf("entry_added changes = %d, want 3", added)
	}
}

// A resume restates the servers its session has: claude reports status only
// for a headless run, so a resumed terminal run must not inherit a status.
func TestSessionContextChain_AResumeEntryNeverInheritsAnMCPStatus(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.applyInitReport(context.Background(), sess.ID, []byte(`{"mcp_servers":[{"name":"task","status":"connected"}]}`), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	resumeAfterEnd(t, svc, sess.ID)
	v := contextOf(t, svc, sess.ID)
	for _, s := range v.Entries[1].MCPServers {
		if s.Status != domain.MCPUnknown || s.StatusReason != domain.MCPStatusUnknownReason {
			t.Errorf("the resume's %s = %q (%q)", s.Name, s.Status, s.StatusReason)
		}
	}
}

// savesOf wraps the service's store so a test can count what is saved.
func savesOf(svc *Service) *savingStore {
	store := &savingStore{SessionContextStore: memory.NewSessionContextStore()}
	svc.Contexts = store
	return store
}

func TestSessionContextChain_AResumeThatFailsStoresNothing(t *testing.T) {
	svc, _ := newInteractiveService(t)
	store := savesOf(svc)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	saved := store.saves

	// Still running: refused.
	if _, _, err := svc.ResumeInteractive(context.Background(), resumeOf(sess.ID)); errs.Code(err) != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("resume of a running session: %v", err)
	}
	// Unknown.
	if _, _, err := svc.ResumeInteractive(context.Background(), resumeOf("nope")); err == nil {
		t.Fatal("resume of an unknown session succeeded")
	}
	// The conversation cannot be resumed.
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	svc.Transcripts = fakeTranscripts{err: errNoConversation}
	if _, _, err := svc.ResumeInteractive(context.Background(), resumeOf(sess.ID)); err == nil {
		t.Fatal("a resume with no conversation succeeded")
	}
	if store.saves != saved {
		t.Errorf("failed resumes stored %d contexts", store.saves-saved)
	}
	if v := contextOf(t, svc, sess.ID); len(v.Entries) != 1 {
		t.Errorf("entries = %d, want only the start", len(v.Entries))
	}
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errNoConversation = constErr("no conversation to resume")

func TestSessionContextChain_AHeadlessStartThatFailsStoresNothing(t *testing.T) {
	svc, _, _ := newTestService(t)
	store := savesOf(svc)
	for name, req := range map[string]StartRequest{
		"unknown project":    {ProjectID: "nope", RepositoryID: "r1", Task: "x"},
		"unknown repository": {ProjectID: "p1", RepositoryID: "nope", Task: "x"},
		"unknown agent":      {ProjectID: "p1", RepositoryID: "r1", Task: "x", AgentID: "nope"},
		"unknown ticket":     {ProjectID: "p1", RepositoryID: "r1", TicketID: "nope", Task: "x"},
		"other project":      {ProjectID: "p1", RepositoryID: "r1", TicketID: "tk2", Task: "x"},
	} {
		if _, err := svc.Start(context.Background(), req); err == nil {
			t.Errorf("%s: the start succeeded", name)
		}
	}
	if store.saves != 0 {
		t.Errorf("failed starts stored %d contexts", store.saves)
	}
	for _, s := range svc.sessions.List(context.Background(), ports.SessionFilter{}) {
		if c, _ := store.Load(context.Background(), s.ID); c != nil {
			t.Errorf("a context was stored for failed start %s", s.ID)
		}
	}
}

func TestSessionContextChain_AStartThatFailsWhileProvisioningStoresNothingAndTellsNoOne(t *testing.T) {
	svc, prov := newInteractiveService(t)
	store := savesOf(svc)
	clock := newFakeClock()
	var sent []sentChange
	svc.contextNotes = newContextNotifier(clock.Now, clock.AfterFunc, func(s, r string) { sent = append(sent, sentChange{s, r}) })
	prov.createErr = errNoConversation
	if _, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"}); err == nil {
		t.Fatal("the start succeeded")
	}
	if store.saves != 0 || len(sent) != 0 {
		t.Errorf("saves = %d, context.changed sent = %v", store.saves, sent)
	}
}

func TestSessionContextChain_ASessionOlderThanTheFeatureIsNotRecordedUntilSomethingRecordsIt(t *testing.T) {
	svc := contextService(t)
	// A store that holds a row with no entries (a cursor or changes left over)
	// is still "nothing recorded".
	if err := svc.Contexts.Save(context.Background(), &domain.SessionContext{SessionID: "s1", Cursor: "9",
		Changes: []domain.ContextChange{{At: time.Unix(1, 0), Kind: domain.ContextChangeSkillInvoked, Subject: "x"}}}); err != nil {
		t.Fatal(err)
	}
	v := contextOf(t, svc, "s1")
	if v.Recorded || len(v.Entries) != 0 || len(v.Changes) != 0 || len(v.Usage.Tools) != 0 {
		t.Errorf("view = %+v", v)
	}
}

func TestSessionContextChain_AServerThatStoresNoContextAnswersNotRecorded(t *testing.T) {
	svc := contextService(t)
	svc.Contexts = nil
	v := contextOf(t, svc, "s1")
	if v.Recorded || v.Entries == nil || v.Changes == nil || v.Usage.Tools == nil {
		t.Errorf("view = %+v", v)
	}
	if _, err := svc.SessionContextPiece(context.Background(), "s1", "e", "p"); errs.Code(err) != "CONTEXT_ENTRY_NOT_FOUND" {
		t.Errorf("piece: %v", err)
	}
	// Nothing else may touch the missing store.
	if err := svc.recordEntry(context.Background(), "s1", entryWith("e", domain.ContextEntryStart, time.Now())); err != nil {
		t.Errorf("recordEntry: %v", err)
	}
	if ok, err := svc.applyInitReport(context.Background(), "s1", []byte(`{"mcp_servers":[{"name":"a","status":"connected"}]}`), nil, time.Now()); ok || err != nil {
		t.Errorf("init: %v %v", ok, err)
	}
	svc.TranscriptReader = &scriptedTranscript{}
	if ok, err := svc.SyncContextUsage(context.Background(), "s1"); ok || err != nil {
		t.Errorf("usage: %v %v", ok, err)
	}
}

func TestSessionContextChain_AnInitReportForASessionNotRecordedChangesNothing(t *testing.T) {
	svc := contextService(t)
	if ok := applyInit(t, svc, `{"claude_code_version":"1.0","mcp_servers":[{"name":"task","status":"connected"}]}`, nil); ok {
		t.Error("an init for an unrecorded session reported a change")
	}
	if c, _ := svc.Contexts.Load(context.Background(), "s1"); c != nil {
		t.Errorf("an init report created a context: %+v", c)
	}
}

func TestSessionContextChain_UsageOfASessionNotRecordedCreatesNoContext(t *testing.T) {
	svc := contextService(t)
	tr := &scriptedTranscript{}
	tr.add(time.Now(), ports.TranscriptToolUse, "Bash", "x")
	svc.TranscriptReader = tr
	syncUsage(t, svc)
	if c, _ := svc.Contexts.Load(context.Background(), "s1"); c != nil {
		t.Errorf("a usage pass created a context for an old session: %+v", c)
	}
}
