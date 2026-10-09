package orchestration

import (
	"context"
	"slices"
	"testing"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestASessionStatusIsAnnouncedOnlyWhenItChanges(t *testing.T) {
	svc, _, _ := newTestService(t)
	bus := eventbus.New()
	svc.Events = bus
	var got []domain.SessionStatusChanged
	ports.On(bus, func(_ context.Context, ev domain.SessionStatusChanged) error {
		got = append(got, ev)
		return nil
	})

	for _, ev := range []SessionEvent{
		{Type: "status", Status: domain.SessionRunning},
		{Type: "status", Status: domain.SessionRunning},
		{Type: "output", Status: domain.SessionThinking},
		{Type: "tool_use", Status: domain.SessionThinking},
		{Type: "tool_result"}, // no status: leaves it as it is
		{Type: "approval_needed", Status: domain.SessionWaitingApproval},
		{Type: "approval_resolved", Status: domain.SessionThinking},
		{Type: "done", Status: domain.SessionDone},
	} {
		svc.Announce("s1", ev)
	}
	svc.Announce("s2", SessionEvent{Type: "approval_needed", Status: domain.SessionWaitingApproval})

	want := []domain.SessionStatusChanged{
		{SessionID: "s1", Status: domain.SessionRunning},
		{SessionID: "s1", Status: domain.SessionThinking},
		{SessionID: "s1", Status: domain.SessionWaitingApproval},
		{SessionID: "s1", Status: domain.SessionThinking},
		{SessionID: "s1", Status: domain.SessionDone},
		{SessionID: "s2", Status: domain.SessionWaitingApproval},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("announced %+v\nwant %+v", got, want)
	}
}
