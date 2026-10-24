package web

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeHelper struct {
	answer ports.WritingHelperAvailability
	calls  int
}

func (f *fakeHelper) Availability(context.Context) ports.WritingHelperAvailability {
	f.calls++
	return f.answer
}

func helperServer(t *testing.T, h ports.WritingHelperReadiness, clock func() time.Time) *server {
	t.Helper()
	deps := Deps{Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "Orion"}}}, Tasks: fakeTickets{}, Sessions: fakeSessions{}, WritingHelper: h, Now: clock}
	s := &server{deps: deps, assets: builtAssets(t)}
	s.helper = newHelperCache(h, clock)
	return s
}

func TestEveryFrameCarriesWhetherTheWritingHelperCanRun(t *testing.T) {
	now := time.Now()
	ok := helperServer(t, &fakeHelper{answer: ports.WritingHelperAvailability{Available: true}}, func() time.Time { return now })
	f, err := ok.layout(context.Background(), "Home", "p1", "")
	if err != nil || f.Helper != (shell.WritingHelper{Available: true}) {
		t.Fatalf("frame helper = %+v, err %v", f.Helper, err)
	}
	no := helperServer(t, &fakeHelper{answer: ports.WritingHelperAvailability{Reason: "no agent credential", FixURL: "/agent-isolation#credential"}}, func() time.Time { return now })
	f, _ = no.layout(context.Background(), "Home", "", "")
	want := shell.WritingHelper{Reason: "no agent credential", FixURL: "/agent-isolation#credential"}
	if f.Helper != want {
		t.Fatalf("frame helper = %+v, want %+v", f.Helper, want)
	}
}

func TestAServerWithoutAWritingHelperSaysItIsUnavailable(t *testing.T) {
	s := helperServer(t, nil, time.Now)
	f, _ := s.layout(context.Background(), "Home", "", "")
	if f.Helper.Available || f.Helper.Reason == "" || f.Helper.FixURL != "" {
		t.Fatalf("frame helper = %+v", f.Helper)
	}
}

// Every page render asks, and the readiness check can start an install, so
// the answer is kept for a moment.
func TestTheAvailabilityIsReadOnceAMomentNotOnEveryPage(t *testing.T) {
	now := time.Now()
	h := &fakeHelper{answer: ports.WritingHelperAvailability{Available: true}}
	s := helperServer(t, h, func() time.Time { return now })
	for range 5 {
		s.layout(context.Background(), "Home", "", "")
	}
	if h.calls != 1 {
		t.Fatalf("readiness checked %d times for 5 pages, want 1", h.calls)
	}
	now = now.Add(helperCacheTTL + time.Millisecond)
	h.answer = ports.WritingHelperAvailability{Reason: "gone"}
	f, _ := s.layout(context.Background(), "Home", "", "")
	if h.calls != 2 || f.Helper.Available || f.Helper.Reason != "gone" {
		t.Fatalf("after the TTL: calls %d, helper %+v", h.calls, f.Helper)
	}
}
