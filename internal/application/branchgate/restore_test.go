package branchgate

import (
	"context"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeSessions map[string]*domain.Session

func (f fakeSessions) Get(id string) *domain.Session { return f[id] }

func (f fakeSessions) List(_ context.Context, flt ports.SessionFilter) []*domain.Session {
	var out []*domain.Session
	for _, s := range f {
		if len(flt.Statuses) == 0 || slices.Contains(flt.Statuses, s.Status) {
			out = append(out, s)
		}
	}
	return out
}

type fakeStore struct{ reqs []*domain.BranchRequest }

func (f *fakeStore) Add(_ context.Context, r *domain.BranchRequest) error {
	f.reqs = append(f.reqs, r)
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*domain.BranchRequest, error) {
	for _, r := range f.reqs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) Save(context.Context, *domain.BranchRequest) error { return nil }

func (f *fakeStore) List(_ context.Context, flt domain.BranchRequestFilter) ([]domain.BranchRequest, error) {
	var out []domain.BranchRequest
	for _, r := range f.reqs {
		if (flt.State == "" || r.State == flt.State) && (flt.SessionID == "" || r.SessionID == flt.SessionID) {
			out = append(out, *r)
		}
	}
	return out, nil
}

// recountWaiter records what the gate hands the session service at boot.
type recountWaiter struct {
	filed    int
	restored map[string]int
}

func (w *recountWaiter) BranchRequestFiled(string)   { w.filed++ }
func (w *recountWaiter) BranchRequestSettled(string) {}
func (w *recountWaiter) BranchRequestsRestored(id string, pending int) {
	if w.restored == nil {
		w.restored = map[string]int{}
	}
	w.restored[id] = pending
}

// A restart recounts a live session's pending requests once, as the number
// stored; it files none again, which would add to the count (ADR 0078).
func TestRestoreRecountsEachLiveSessionFromItsStoredPendingRequests(t *testing.T) {
	store := &fakeStore{reqs: []*domain.BranchRequest{
		{ID: "a", SessionID: "tui", State: domain.BranchRequestPending},
		{ID: "b", SessionID: "tui", State: domain.BranchRequestPending},
		{ID: "c", SessionID: "gone", State: domain.BranchRequestPending},
	}}
	sessions := fakeSessions{
		"tui":   {ID: "tui", Status: domain.SessionWaitingApproval, PendingApprovals: 5},
		"quiet": {ID: "quiet", Status: domain.SessionWaitingApproval, PendingApprovals: 1},
		"gone":  {ID: "gone", Status: domain.SessionStopped},
	}
	w := &recountWaiter{}
	svc := NewService(Deps{Sessions: sessions, Requests: store, Waiter: w})

	if err := svc.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.filed != 0 {
		t.Errorf("Restore filed %d requests again, want none", w.filed)
	}
	if got := w.restored["tui"]; got != 2 {
		t.Errorf("tui recounted as %d, want 2", got)
	}
	if n, ok := w.restored["quiet"]; !ok || n != 0 {
		t.Errorf("quiet recounted as %d (seen %v), want 0", n, ok)
	}
	if _, ok := w.restored["gone"]; ok {
		t.Error("an ended session was recounted")
	}
}
