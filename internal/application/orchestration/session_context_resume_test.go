package orchestration

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func entryWith(id, kind string, at time.Time, pieces ...domain.ContextPiece) domain.ContextEntry {
	return domain.ContextEntry{ID: id, Kind: kind, At: at, Instructions: pieces}
}

func fileP(path, version, text string) domain.ContextPiece {
	return domain.NewContextPiece(path, domain.PieceInstructionFile, path, "Instruction files", path, version, text)
}

func textP(id, text string) domain.ContextPiece {
	return domain.NewContextPiece(id, domain.PieceBrief, id, "Harness", "", "", text)
}

func TestSessionContext_AResumeEntryMarksWhatChangedSinceTheEntryBefore(t *testing.T) {
	svc := contextService(t)
	start := time.Unix(100, 0)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, start,
		fileP("CLAUDE.md", "3", "root memory"),
		fileP("web/AGENTS.md", "1", "web"),
		textP("task-brief", "brief v1"),
		textP("services", "same"),
	))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, start.Add(time.Hour),
		fileP("CLAUDE.md", "4", "root memory, edited"), // versioned, new version
		fileP("web/AGENTS.md", "1", "web"),             // same version
		textP("task-brief", "brief v2"),                // unversioned, new text
		textP("services", "same"),                      // unversioned, same text
		fileP("NEW.md", "1", "new to this entry"),      // no previous
	))

	v := contextOf(t, svc, "s1")
	if len(v.Entries) != 2 || v.Entries[0].ID != "e1" || v.Entries[1].ID != "e2" {
		t.Fatalf("entries = %+v", v.Entries)
	}
	for _, p := range v.Entries[0].Instructions {
		if p.Changed != nil {
			t.Errorf("a start entry's piece %s is marked changed: %+v", p.ID, p.Changed)
		}
	}
	by := map[string]domain.ContextPiece{}
	for _, p := range v.Entries[1].Instructions {
		by[p.ID] = p
	}
	if c := by["CLAUDE.md"].Changed; c == nil || c.FromVersion != "3" || c.ToVersion != "4" {
		t.Errorf("CLAUDE.md changed = %+v", c)
	}
	if by["web/AGENTS.md"].Changed != nil {
		t.Errorf("same version marked changed: %+v", by["web/AGENTS.md"].Changed)
	}
	c := by["task-brief"].Changed
	if c == nil || c.FromVersion == "" || c.ToVersion == "" || c.FromVersion == c.ToVersion || len(c.FromVersion) > 12 {
		t.Errorf("unversioned text change = %+v (want short content hashes)", c)
	}
	if by["services"].Changed != nil {
		t.Errorf("same text marked changed: %+v", by["services"].Changed)
	}
	if by["NEW.md"].Changed != nil {
		t.Errorf("a piece new to the entry has no previous: %+v", by["NEW.md"].Changed)
	}
}

func TestSessionContext_EachEntryAddsAnEntryAddedChange(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, entryWith("e1", domain.ContextEntryStart, time.Unix(100, 0)))
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Unix(200, 0)))
	v := contextOf(t, svc, "s1")
	if len(v.Changes) != 2 {
		t.Fatalf("changes = %+v", v.Changes)
	}
	if v.Changes[0].Kind != domain.ContextChangeEntryAdded || v.Changes[0].Subject != "e2" ||
		v.Changes[1].Subject != "e1" || v.Changes[0].Detail == "" {
		t.Errorf("changes (newest first) = %+v", v.Changes)
	}
}

func TestSessionContext_AResumeKeepsTheStartAndAddsAnEntryOfItsOwn(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	before := contextOf(t, svc, sess.ID)

	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	v := contextOf(t, svc, sess.ID)
	if len(v.Entries) != 2 {
		t.Fatalf("entries = %d, want the start and the resume", len(v.Entries))
	}
	if v.Entries[0].ID != before.Entries[0].ID || v.Entries[0].Kind != domain.ContextEntryStart {
		t.Errorf("the start entry changed: %+v", v.Entries[0])
	}
	resume := v.Entries[1]
	if resume.Kind != domain.ContextEntryResume || resume.Facts.Headless || resume.Facts.Branch != sess.Branch {
		t.Errorf("resume entry = %+v", resume.Facts)
	}
	if p, ok := resume.Piece("task-brief"); !ok || p.Text == "" {
		t.Errorf("the resume's brief = %+v (found %v)", p, ok)
	}
	if _, ok := serverByName(resume, "task"); !ok {
		t.Error("the resume's servers are missing")
	}
}
