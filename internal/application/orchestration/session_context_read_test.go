package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
)

// contextService is a service with a session "s1" and a context store.
func contextService(t *testing.T) *Service {
	t.Helper()
	svc, _, _ := newTestService(t)
	svc.Contexts = memory.NewSessionContextStore()
	if _, err := svc.sessions.Create(&domain.Session{ID: "s1", ProjectID: "p1", Task: "t", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func storeEntry(t *testing.T, svc *Service, e domain.ContextEntry) {
	t.Helper()
	if err := svc.recordEntry(context.Background(), "s1", e); err != nil {
		t.Fatal(err)
	}
}

func TestSessionContext_ASessionFromBeforeRecordingReadsAsNotRecorded(t *testing.T) {
	svc := contextService(t)
	v, err := svc.SessionContext(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Recorded || v.SessionID != "s1" || v.Entries == nil || len(v.Entries) != 0 || len(v.Changes) != 0 ||
		v.Usage.Tools == nil || v.Usage.Skills == nil {
		t.Errorf("view = %+v", v)
	}
}

func TestSessionContext_AnUnknownSessionIsRefused(t *testing.T) {
	svc := contextService(t)
	_, err := svc.SessionContext(context.Background(), "nope")
	if errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("err = %v", err)
	}
}

func TestSessionContext_AStoredEntryReadsBackWithItsPiecesCutAt4KB(t *testing.T) {
	svc := contextService(t)
	long := strings.Repeat("é", 3000) // 6000 bytes, each rune 2 bytes: the cut at 4096 lands on a boundary
	odd := "a" + strings.Repeat("é", 3000)
	storeEntry(t, svc, domain.ContextEntry{
		ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(10, 0),
		Instructions: []domain.ContextPiece{
			domain.NewContextPiece("p-long", domain.PieceBrief, "Task brief", "Harness", "", "", long),
			domain.NewContextPiece("p-odd", domain.PieceOther, "Odd", "Harness", "", "", odd),
			domain.NewContextPiece("p-short", domain.PieceOther, "Short", "Harness", "", "", "hi"),
		},
	})
	v, err := svc.SessionContext(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Recorded || len(v.Entries) != 1 {
		t.Fatalf("view = %+v", v)
	}
	by := map[string]domain.ContextPiece{}
	for _, p := range v.Entries[0].Instructions {
		by[p.ID] = p
	}
	long1 := by["p-long"]
	if len(long1.Text) != 4096 || !long1.Truncated || long1.Bytes != 6000 || long1.ApproxTokens != 1500 {
		t.Errorf("long = text %d bytes, truncated %v, bytes %d, tokens %d", len(long1.Text), long1.Truncated, long1.Bytes, long1.ApproxTokens)
	}
	odd1 := by["p-odd"]
	if len(odd1.Text) != 4095 || !odd1.Truncated {
		t.Errorf("odd cut = %d bytes (a multibyte character must not be split), truncated %v", len(odd1.Text), odd1.Truncated)
	}
	short := by["p-short"]
	if short.Text != "hi" || short.Truncated || short.Bytes != 2 {
		t.Errorf("short = %+v", short)
	}
	if v.Entries[0].Tools == nil || v.Entries[0].MCPServers == nil || v.Entries[0].Skills == nil || v.Entries[0].Hooks == nil {
		t.Errorf("lists must be [] not null: %+v", v.Entries[0])
	}
}

func TestSessionContext_APieceAnswersItsWholeText(t *testing.T) {
	svc := contextService(t)
	long := strings.Repeat("x", 9000)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Now(),
		Instructions: []domain.ContextPiece{domain.NewContextPiece("p1", domain.PieceBrief, "Task brief", "H", "", "", long)}})
	got, err := svc.SessionContextPiece(context.Background(), "s1", "e1", "p1")
	if err != nil || got != long {
		t.Errorf("text = %d bytes, err %v", len(got), err)
	}
}

func TestSessionContext_APieceLookupRefusesWhatIsUnknown(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Now(),
		Instructions: []domain.ContextPiece{domain.NewContextPiece("p1", domain.PieceBrief, "T", "H", "", "", "x")}})
	for _, c := range []struct{ session, entry, piece, code string }{
		{"nope", "e1", "p1", "SESSION_NOT_FOUND"},
		{"s1", "nope", "p1", "CONTEXT_ENTRY_NOT_FOUND"},
		{"s1", "e1", "nope", "CONTEXT_PIECE_NOT_FOUND"},
	} {
		_, err := svc.SessionContextPiece(context.Background(), c.session, c.entry, c.piece)
		if errs.Code(err) != c.code {
			t.Errorf("%v: err = %v, want %s", c, err, c.code)
		}
	}
	// A session with no recorded context has no entry either.
	if _, err := svc.sessions.Create(&domain.Session{ID: "s2", ProjectID: "p1", Task: "t", Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SessionContextPiece(context.Background(), "s2", "e1", "p1"); errs.Code(err) != "CONTEXT_ENTRY_NOT_FOUND" {
		t.Errorf("unrecorded: err = %v", err)
	}
}

func TestSessionContext_DeletingTheSessionDeletesItsContext(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Now()})
	if err := svc.Delete(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if c, err := svc.Contexts.Load(context.Background(), "s1"); err != nil || c != nil {
		t.Errorf("context after delete = %+v, %v", c, err)
	}
}
