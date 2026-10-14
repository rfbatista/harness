package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gormsqlite "gorm.io/driver/sqlite"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Load of the inbox test, on top of the freeze test's streaming sessions:
// inboxPosters agents each post the most notifications an agent may post in
// an hour, while every session is announced waiting for approval and running
// again statusRounds times.
const (
	inboxPosters = 24
	statusRounds = 15
	// lockHeld is how long another connection holds the database's write
	// lock in the middle of the writes, so they wait on it.
	lockHeld = 2 * time.Second
)

// inboxStream is one inbox SSE stream held open over HTTP.
type inboxStream struct {
	changes chan ports.NotificationChange
	cancel  context.CancelFunc
}

// openInbox opens the inbox stream and answers once the server has answered
// its headers, with how long that took.
func (e *freezeE2E) openInbox() (*inboxStream, time.Duration) {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.srv.URL+"/api/notifications/events", nil)
	start := time.Now()
	res, err := http.DefaultClient.Do(req)
	took := time.Since(start)
	if err != nil {
		cancel()
		e.t.Fatalf("open the inbox stream: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		cancel()
		e.t.Fatalf("open the inbox stream: %d", res.StatusCode)
	}
	s := &inboxStream{changes: make(chan ports.NotificationChange, 4096), cancel: cancel}
	go func() {
		defer res.Body.Close()
		defer close(s.changes)
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var c ports.NotificationChange
			if err := json.Unmarshal([]byte(data), &c); err != nil {
				if ctx.Err() != nil {
					return // closed by the test mid-line
				}
				e.t.Errorf("inbox stream sent %q: %v", data, err)
				return
			}
			s.changes <- c
		}
	}()
	return s, took
}

// While sessions stream events into a database file and agents post
// notifications and sessions wait for approval and resume, the inbox stream
// receives every change in commit order, each with the unread count as of
// its commit, and a newly opened inbox stream answers promptly.
func TestInboxKeepsFlowingWhileNotificationsAreWrittenOnAFileDatabase(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "data.db")
	e := startFreezeServer(t, dir, dbPath, true)

	ids := make([]string, concurrentSessions)
	for i := range ids {
		sess, err := e.orch.Start(e.ctx, orchestration.StartRequest{
			ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket,
			Task: fmt.Sprintf("stream %d", i), Branch: fmt.Sprintf("inbox-%d", i), AutoAccept: "all",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = sess.ID
		t.Cleanup(func() { _ = e.orch.Stop(context.Background(), sess.ID) })
	}

	inbox, _ := e.openInbox()
	defer inbox.cancel()

	var (
		postedMu sync.Mutex
		posted   = map[string]bool{}
		writers  sync.WaitGroup
	)
	for p := range inboxPosters {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := range 20 {
				n, err := e.notes.Notify(e.ctx, ports.NotifyRequest{
					SessionID: fmt.Sprintf("poster-%d", p), ProjectID: e.project,
					Title: fmt.Sprintf("note %d from poster %d", i, p),
				})
				if err != nil {
					t.Errorf("notify: %v", err)
					return
				}
				postedMu.Lock()
				posted[n.ID] = true
				postedMu.Unlock()
			}
		}()
	}
	for _, id := range ids {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range statusRounds {
				for _, st := range []domain.SessionStatus{domain.SessionWaitingApproval, domain.SessionRunning} {
					if err := e.bus.Publish(e.ctx, domain.SessionStatusChanged{SessionID: id, Status: st}); err != nil {
						t.Errorf("announce %s %s: %v", id, st, err)
					}
				}
			}
		}()
	}
	writers.Add(1)
	go func() {
		defer writers.Done()
		time.Sleep(200 * time.Millisecond)
		holdWriteLock(t, dbPath, lockHeld)
	}()
	writing := make(chan struct{})
	go func() {
		writers.Wait()
		close(writing)
	}()

	// Opening a second inbox stream during the writes answers within the bound.
	opened, slowestOpen := 0, time.Duration(0)
	for done := false; !done; {
		select {
		case <-writing:
			done = true
		case <-time.After(50 * time.Millisecond):
			second, took := e.openInbox()
			second.cancel()
			opened++
			slowestOpen = max(slowestOpen, took)
			if took > readBound {
				t.Errorf("opening an inbox stream during the writes took %v, over %v", took, readBound)
			}
		}
	}
	if t.Failed() {
		return
	}

	// The held stream saw every change, in order, with a consistent count.
	var page struct {
		UnreadCount int `json:"unread_count"`
	}
	if err := json.Unmarshal(e.get("/api/notifications?limit=1"), &page); err != nil {
		t.Fatal(err)
	}
	unread, changes := 0, 0
	state := map[string]string{} // id → "unread" or "resolved"
	seen := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for len(seen) < len(posted) || unread != page.UnreadCount {
		var c ports.NotificationChange
		select {
		case got, ok := <-inbox.changes:
			if !ok {
				t.Fatalf("the inbox stream ended after %d changes", changes)
			}
			c = got
		case <-deadline:
			t.Fatalf("the inbox stream stalled: %d changes, %d of %d posted seen, unread %d, inbox says %d",
				changes, len(seen), len(posted), unread, page.UnreadCount)
		}
		changes++
		for _, n := range c.Changed {
			switch {
			case n.ResolvedAt != nil:
				if state[n.ID] != "unread" {
					t.Fatalf("change %d resolved %s, which the stream never showed unread", changes, n.ID)
				}
				state[n.ID] = "resolved"
				unread--
			case n.ReadAt == nil:
				if state[n.ID] != "" {
					t.Fatalf("change %d created %s twice", changes, n.ID)
				}
				state[n.ID] = "unread"
				unread++
			}
			if posted[n.ID] {
				seen[n.ID] = true
			}
		}
		if c.UnreadCount != unread {
			t.Fatalf("change %d says %d unread; the changes so far make %d", changes, c.UnreadCount, unread)
		}
	}
	t.Logf("%d changes on the held stream, %d streams opened during the writes (slowest %v), %d unread", changes, opened, slowestOpen, unread)
}

// holdWriteLock takes the write lock of the database at dbPath on a
// connection of its own and keeps it for d, so every write waits on it.
func holdWriteLock(t *testing.T, dbPath string, d time.Duration) {
	db, err := sql.Open(gormsqlite.DriverName, dbPath+"?_busy_timeout=5000")
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Error(err)
		return
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Errorf("take the write lock: %v", err)
		return
	}
	time.Sleep(d)
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Errorf("release the write lock: %v", err)
	}
}
