package bench

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type capturingSessions struct{ setups chan ports.BenchSetupRequest }

func (c capturingSessions) Setup(_ context.Context, r ports.BenchSetupRequest) (ports.BenchSetupInfo, error) {
	c.setups <- r
	return ports.BenchSetupInfo{Model: r.Model}, nil
}
func (c capturingSessions) Run(context.Context, ports.BenchSessionRequest) (ports.BenchSessionResult, error) {
	return ports.BenchSessionResult{FinalText: "x"}, nil
}

func TestARunWithNoModelUsesTheModelSessionsRunOn(t *testing.T) {
	cases := &memCases{cases: []domain.BenchCase{{ID: "c", Prompt: "p"}}}
	sessions := capturingSessions{setups: make(chan ports.BenchSetupRequest, 2)}
	svc := New(Deps{Cases: cases, Projects: projects{"p1": true}, Sessions: sessions,
		ServeMock: func(*MockLayer) (string, func(), error) { return "", func() {}, nil }, SessionModel: func() string { return "sonnet-default" }})
	if _, err := svc.StartRun(context.Background(), "p1", domain.BenchRunRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := <-sessions.setups; got.Model != "sonnet-default" {
		t.Fatalf("model = %q", got.Model)
	}
}

func TestComparisonSkipsACancelledPreviousRun(t *testing.T) {
	store := &memCards{}
	t0 := time.Now()
	store.cards = []domain.Scorecard{
		{ID: "cur", ProjectID: "p1", Status: domain.ScorecardFinished, StartedAt: t0.Add(2 * time.Hour)},
		{ID: "partial", ProjectID: "p1", Status: domain.ScorecardCancelled, StartedAt: t0.Add(time.Hour)},
		{ID: "base", ProjectID: "p1", Status: domain.ScorecardFinished, StartedAt: t0},
	}
	svc := New(Deps{Scorecards: store, Projects: projects{"p1": true}})
	cmp, err := svc.CompareScorecard(context.Background(), "p1", "cur", "")
	if err != nil || cmp.PreviousID != "base" {
		t.Fatalf("previous = %q, %v", cmp.PreviousID, err)
	}
}

type memCards struct{ cards []domain.Scorecard }

func (m *memCards) Save(context.Context, domain.Scorecard) error { return nil }
func (m *memCards) Get(_ context.Context, _, id string) (*domain.Scorecard, error) {
	for i := range m.cards {
		if m.cards[i].ID == id {
			return &m.cards[i], nil
		}
	}
	return nil, nil
}
func (m *memCards) Find(ctx context.Context, id string) (*domain.Scorecard, error) {
	return m.Get(ctx, "", id)
}
func (m *memCards) List(context.Context, string) ([]domain.Scorecard, error) { return m.cards, nil }
func (m *memCards) DeleteProject(context.Context, string) error              { return nil }
