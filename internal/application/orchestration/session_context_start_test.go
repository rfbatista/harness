package orchestration

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
)

func contextOf(t *testing.T, svc *Service, id string) domain.SessionContextView {
	t.Helper()
	v, err := svc.SessionContext(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func pieceOf(e domain.ContextEntry, id string) (domain.ContextPiece, bool) {
	return e.Piece(id)
}

func TestSessionContext_ATerminalStartStoresItsFactsAndInstructions(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "begin with the tests"})

	v := contextOf(t, svc, sess.ID)
	if !v.Recorded || len(v.Entries) != 1 {
		t.Fatalf("view = %+v", v)
	}
	e := v.Entries[0]
	if e.Kind != domain.ContextEntryStart || e.ID == "" || e.At.IsZero() {
		t.Errorf("entry = %+v", e)
	}
	if e.Facts.Headless || e.Facts.Branch != sess.Branch || e.Facts.Worktree != sess.WorkingDir ||
		e.Facts.Sandbox != "off" || e.Facts.TokenSavers == nil {
		t.Errorf("facts = %+v", e.Facts)
	}
	brief, ok := pieceOf(e, "task-brief")
	if !ok || brief.Kind != domain.PieceBrief || !strings.Contains(brief.Text, "Ship the thing") {
		t.Errorf("task brief piece = %+v (found %v)", brief, ok)
	}
	opening, ok := pieceOf(e, "opening-message")
	if !ok || !strings.Contains(opening.Text, "begin with the tests") {
		t.Errorf("opening piece = %+v (found %v)", opening, ok)
	}
	// The brief is sent before the opening message.
	if idx := func() int {
		for i, p := range e.Instructions {
			if p.ID == "task-brief" {
				return i
			}
		}
		return -1
	}(); idx < 0 || e.Instructions[len(e.Instructions)-1].ID != "opening-message" {
		t.Errorf("order = %+v", e.Instructions)
	}
	if e.Facts.ApproxTokens == 0 {
		t.Error("approx tokens not summed")
	}
}

func TestSessionContext_AHeadlessStartStoresItsTaskAsTheOpeningMessage(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Contexts = memory.NewSessionContextStore()
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello there"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	v := contextOf(t, svc, d.ID)
	if len(v.Entries) != 1 || !v.Entries[0].Facts.Headless {
		t.Fatalf("view = %+v", v)
	}
	if p, ok := pieceOf(v.Entries[0], "opening-message"); !ok || p.Text != "hello there" {
		t.Errorf("opening = %+v (found %v)", p, ok)
	}
}

// savingStore counts the contexts saved to it.
type savingStore struct {
	*memory.SessionContextStore
	saves int
}

func (s *savingStore) Save(ctx context.Context, c *domain.SessionContext) error {
	s.saves++
	return s.SessionContextStore.Save(ctx, c)
}

func TestSessionContext_AStartThatFailsBeforeClaudeRunsStoresNothing(t *testing.T) {
	svc, _ := newInteractiveService(t)
	store := &savingStore{SessionContextStore: memory.NewSessionContextStore()}
	svc.Contexts = store
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "x"})
	if codeOf(err) != "AGENT_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
	if store.saves != 0 {
		t.Errorf("a failed start stored %d contexts", store.saves)
	}
}
