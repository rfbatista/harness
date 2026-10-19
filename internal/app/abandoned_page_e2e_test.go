//go:build !windows

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A page whose request ends while every reader connection is busy stops
// waiting for one: it returns at once, and once the readers are free again it
// holds none of them. The agent session events published meanwhile, on the
// writer connection, are all stored: a page closing never loses one.
func TestAnAbandonedPageStopsItsDatabaseWork(t *testing.T) {
	b := newBudgetProject(t)
	b.addSessions(10)
	live, err := b.sessions.Create(&domain.Session{ID: "s-live", ProjectID: b.project, RepositoryID: b.repo, TicketID: b.tickets[0],
		Task: "Work on the task", Status: domain.SessionRunning, RunsOn: domain.RunnerServer, Mode: domain.SessionModeImplement})
	if err != nil {
		t.Fatal(err)
	}

	readers, err := sqlite.Reader(b.db).DB()
	if err != nil {
		t.Fatal(err)
	}
	held := make([]interface{ Close() error }, 0, readers.Stats().MaxOpenConnections)
	for range readers.Stats().MaxOpenConnections {
		c, err := readers.Conn(b.ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	release := sync.OnceFunc(func() {
		for _, c := range held {
			_ = c.Close()
		}
	})
	defer release()

	pages := []string{
		"/projects/" + b.project + "/tasks/" + b.tickets[0],
		"/projects/" + b.project,
		"/projects/" + b.project + "/reviews",
		"/projects/" + b.project + "/repositories",
		"/projects/" + b.project + "/tasks/" + b.tickets[0] + "/sessions/s-000/history",
	}
	ctx, cancel := context.WithCancel(b.ctx)
	var loads sync.WaitGroup
	for _, path := range pages {
		loads.Add(1)
		go func() {
			defer loads.Done()
			b.handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
		}()
	}

	const events = 20
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := range events {
			b.orch.Announce(live.ID, ports.SessionEvent{Type: "output", Text: fmt.Sprintf("line %d", i)})
		}
	}()

	time.Sleep(100 * time.Millisecond) // the pages are waiting for a reader
	cancel()
	returned := make(chan struct{})
	go func() { loads.Wait(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		release()
		<-returned
		t.Fatal("the abandoned pages kept waiting for a reader connection after their request ended")
	}

	release()
	<-published
	deadline := time.Now().Add(2 * time.Second)
	for readers.Stats().InUse > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := readers.Stats().InUse; n > 0 {
		t.Errorf("%d reader connections are still in use after the abandoned pages returned", n)
	}

	stored, err := b.sessions.ListEvents(live.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != events {
		t.Errorf("%d of the %d events published while the page was abandoned were stored", len(stored), events)
	}
}
