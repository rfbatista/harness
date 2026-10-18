package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// costPerTurn is what each of streamingClaude's turn results costs.
const costPerTurn = 0.001

// With several sessions streaming into a database file, each session's stored
// metrics never run ahead of its history, and once the sessions stop, every
// event each one emitted is in its history, in order, with the session's
// status and metrics matching those events and its live stream agreeing.
func TestEachAgentEventIsStoredWithItsMetricsAndStatus(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler)) // a line per event
	t.Cleanup(func() { slog.SetDefault(prev) })
	dir := t.TempDir()
	e := startFreezeE2E(t, dir, filepath.Join(dir, "data.db"))
	ids := e.startSessions(concurrentSessions)

	streams := make([]*liveStream, 3)
	for i := range streams {
		streams[i] = e.follow(ids[i])
	}

	deadline := time.Now().Add(loadDuration)
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) && !t.Failed() {
				for _, id := range ids {
					// The session first, then its history: whatever the
					// session's metrics already count, the history must hold.
					sess := e.session(id)
					h := e.history(id)
					if want := costPerTurn * float64(countType(h, "usage")); sess.CostUSD > want+1e-6 {
						t.Errorf("session %s costs %v, ahead of the %v its history's usage events add up to", id, sess.CostUSD, want)
					}
					if sess.LastAction != "" && !hasAction(h, sess.LastAction) {
						t.Errorf("session %s last action %q is not in its history", id, sess.LastAction)
					}
				}
			}
		}()
	}
	wg.Wait()

	for _, id := range ids {
		if err := e.orch.Stop(context.Background(), id); err != nil {
			t.Fatalf("stop %s: %v", id, err)
		}
	}
	for i, id := range ids {
		h := e.historyUntilDone(id)
		checkHistoryInOrder(t, id, h)

		sess := e.session(id)
		if want := costPerTurn * float64(countType(h, "usage")); math.Abs(sess.CostUSD-want) > 1e-6 {
			t.Errorf("session %s stored cost %v, want %v for its %d usage events", id, sess.CostUSD, want, countType(h, "usage"))
		}
		if want := lastAction(h); sess.LastAction != want {
			t.Errorf("session %s stored last action %q, want %q", id, sess.LastAction, want)
		}
		if want := lastStatus(h); sess.Status != want {
			t.Errorf("session %s stored status %q, want %q", id, sess.Status, want)
		}
		if i < len(streams) {
			streams[i].agreesWith(t, id, h)
		}
	}
}

// A failure to store a session's agent event is logged naming the session:
// here the database closes under sessions that are still streaming.
func TestAStoreFailureOnTheSessionEventPathIsLoggedWithTheSession(t *testing.T) {
	prev := slog.Default()
	logs := &errorLog{}
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prev) })
	dir := t.TempDir()
	e := startFreezeE2E(t, dir, filepath.Join(dir, "data.db"))
	ids := e.startSessions(2)
	for _, id := range ids {
		for len(e.history(id)) < 10 {
			time.Sleep(20 * time.Millisecond)
		}
	}

	if err := sqlite.Close(e.db); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !logs.names(ids...) {
		time.Sleep(20 * time.Millisecond)
	}
	for _, id := range ids {
		_ = e.orch.Stop(context.Background(), id)
	}
	if !logs.names(ids...) {
		t.Errorf("no error log names every session whose events could not be stored; got:\n%s", logs.String())
	}
}

// startSessions starts n sessions of streamingClaude on the e2e project.
func (e *freezeE2E) startSessions(n int) []string {
	e.t.Helper()
	ids := make([]string, n)
	for i := range ids {
		sess, err := e.orch.Start(e.ctx, orchestration.StartRequest{
			ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket,
			Task: fmt.Sprintf("stream %d", i), Branch: fmt.Sprintf("freeze-%d", i), AutoAccept: "all",
		})
		if err != nil {
			e.t.Fatal(err)
		}
		ids[i] = sess.ID
		e.t.Cleanup(func() { _ = e.orch.Stop(context.Background(), sess.ID) })
	}
	return ids
}

func (e *freezeE2E) session(id string) domain.Session {
	var body struct {
		Session domain.Session `json:"session"`
	}
	if err := json.Unmarshal(e.get("/api/sessions/"+id), &body); err != nil {
		e.t.Errorf("session %s: %v", id, err)
	}
	return body.Session
}

func (e *freezeE2E) history(id string) []ports.SessionEvent {
	var body struct {
		Events []ports.SessionEvent `json:"events"`
	}
	if err := json.Unmarshal(e.get("/api/sessions/"+id+"/history"), &body); err != nil {
		e.t.Errorf("history %s: %v", id, err)
	}
	return body.Events
}

// historyUntilDone waits for a stopped session's history to end with its
// "done" event, the last one its event loop stores.
func (e *freezeE2E) historyUntilDone(id string) []ports.SessionEvent {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h := e.history(id)
		if n := len(h); n > 0 && h[n-1].Type == "done" {
			return h
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("session %s history never ended with its done event (%d events)", id, len(h))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// checkHistoryInOrder checks h is numbered 1, 2, 3... and holds every turn
// streamingClaude wrote, in the order it wrote them: "working 0", its tool
// use and its usage, then "working 1", and so on.
func checkHistoryInOrder(t *testing.T, id string, h []ports.SessionEvent) {
	t.Helper()
	turn, step := 0, 0
	for i, ev := range h {
		if ev.Seq != int64(i+1) {
			t.Errorf("session %s event %d has seq %d, want %d", id, i, ev.Seq, i+1)
			return
		}
		switch ev.Type {
		case "output", "tool_use", "usage":
		default:
			continue
		}
		want := [...]string{"output", "tool_use", "usage"}[step]
		if ev.Type != want {
			t.Errorf("session %s seq %d is %s, want %s of turn %d", id, ev.Seq, ev.Type, want, turn)
			return
		}
		if ev.Type == "output" && ev.Text != fmt.Sprintf("working %d", turn) {
			t.Errorf("session %s seq %d says %q, want %q", id, ev.Seq, ev.Text, fmt.Sprintf("working %d", turn))
			return
		}
		if step = (step + 1) % 3; step == 0 {
			turn++
		}
	}
	if turn == 0 {
		t.Errorf("session %s stored no whole turn", id)
	}
}

func countType(h []ports.SessionEvent, typ string) int {
	n := 0
	for _, ev := range h {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// action is the last action an output or tool use event leaves on its
// session, or "" for any other event.
func action(ev ports.SessionEvent) string {
	switch ev.Type {
	case "output":
		return ev.Text
	case "tool_use":
		return "tool: " + ev.ToolName
	}
	return ""
}

func hasAction(h []ports.SessionEvent, want string) bool {
	for _, ev := range h {
		if action(ev) == want {
			return true
		}
	}
	return false
}

func lastAction(h []ports.SessionEvent) string {
	for i := len(h) - 1; i >= 0; i-- {
		if a := action(h[i]); a != "" {
			return a
		}
	}
	return ""
}

func lastStatus(h []ports.SessionEvent) domain.SessionStatus {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Status != "" {
			return h[i].Status
		}
	}
	return ""
}

// liveStream is what a client following a session's event stream saw.
type liveStream struct {
	mu     sync.Mutex
	events []ports.SessionEvent
}

// follow opens id's live event stream and records its events until the test
// ends.
func (e *freezeE2E) follow(id string) *liveStream {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.srv.URL+"/api/sessions/"+id+"/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	s := &liveStream{}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev ports.SessionEvent
			if json.Unmarshal([]byte(data), &ev) == nil && ev.Seq > 0 {
				s.mu.Lock()
				s.events = append(s.events, ev)
				s.mu.Unlock()
			}
		}
	}()
	return s
}

// agreesWith checks the live stream saw its events in seq order, and that
// each is the event the history holds under that seq.
func (s *liveStream) agreesWith(t *testing.T, id string, h []ports.SessionEvent) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		t.Errorf("session %s live stream saw no event", id)
	}
	var last int64
	for _, ev := range s.events {
		if ev.Seq <= last {
			t.Errorf("session %s live stream saw seq %d after %d", id, ev.Seq, last)
			return
		}
		last = ev.Seq
		if ev.Seq > int64(len(h)) {
			t.Errorf("session %s live stream saw seq %d, history has %d events", id, ev.Seq, len(h))
			return
		}
		got := h[ev.Seq-1]
		if got.Type != ev.Type || got.Text != ev.Text || got.ToolName != ev.ToolName || got.Status != ev.Status {
			t.Errorf("session %s seq %d: live %s %q %q %q, history %s %q %q %q", id, ev.Seq,
				ev.Type, ev.Text, ev.ToolName, ev.Status, got.Type, got.Text, got.ToolName, got.Status)
			return
		}
	}
}

// errorLog keeps the error records logged, with their attributes.
type errorLog struct {
	mu   sync.Mutex
	recs []string
}

func (l *errorLog) Enabled(_ context.Context, lv slog.Level) bool { return lv >= slog.LevelError }
func (l *errorLog) WithAttrs([]slog.Attr) slog.Handler            { return l }
func (l *errorLog) WithGroup(string) slog.Handler                 { return l }

func (l *errorLog) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	l.mu.Lock()
	l.recs = append(l.recs, b.String())
	l.mu.Unlock()
	return nil
}

// names reports whether, for each id, some record carries session=id.
func (l *errorLog) names(ids ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, id := range ids {
		found := false
		for _, r := range l.recs {
			if strings.Contains(r, " session="+id) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (l *errorLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.recs) > 20 {
		return strings.Join(l.recs[:20], "\n") + "\n..."
	}
	return strings.Join(l.recs, "\n")
}
