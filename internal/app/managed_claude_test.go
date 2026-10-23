package app

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type listingSessions struct {
	ports.SessionRepository
	asked ports.SessionFilter
}

func (l *listingSessions) List(_ context.Context, f ports.SessionFilter) []*domain.Session {
	l.asked = f
	return []*domain.Session{{}, {}}
}

func TestTheRunningSessionCounterCountsOnlySessionsWithAProcess(t *testing.T) {
	repo := &listingSessions{}
	if n := runningSessionCounter(repo)(); n != 2 {
		t.Fatalf("count = %d, want the 2 the repository listed", n)
	}
	for _, s := range repo.asked.Statuses {
		if s.IsTerminal() || s == domain.SessionPaused {
			t.Errorf("status %q has no running process", s)
		}
	}
	if len(repo.asked.Statuses) == 0 {
		t.Error("an empty status filter counts every session ever started")
	}
}
