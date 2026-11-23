package orchestration

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeClock is a clock a test moves: timers fire when it passes them.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at time.Time
	fn func()
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timers = append(c.timers, &fakeTimer{at: c.now.Add(d), fn: fn})
}

// Advance moves the clock and runs the timers that came due, in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	rest := c.timers[:0]
	for _, t := range c.timers {
		if !t.at.After(c.now) {
			due = append(due, t)
		} else {
			rest = append(rest, t)
		}
	}
	c.timers = rest
	c.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, t := range due {
		t.fn()
	}
}

type sentChange struct{ session, reason string }

func notifierWith(clock *fakeClock) (*contextNotifier, *[]sentChange) {
	var sent []sentChange
	var mu sync.Mutex
	n := newContextNotifier(clock.Now, clock.AfterFunc, func(session, reason string) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, sentChange{session, reason})
	})
	return n, &sent
}

func TestContextNotifier_AFirstChangeIsSentAtOnce(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "entry_added")
	if len(*sent) != 1 || (*sent)[0] != (sentChange{"s1", "entry_added"}) {
		t.Errorf("sent = %v", *sent)
	}
}

func TestContextNotifier_ABurstInsideTheWindowIsOneMessageAtItsEndAndTheLastChangeIsNotLost(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "usage")
	clock.Advance(500 * time.Millisecond)
	n.changed("s1", "usage")
	clock.Advance(500 * time.Millisecond)
	n.changed("s1", "mcp_status")
	n.changed("s1", "usage")
	if len(*sent) != 1 {
		t.Fatalf("inside the window: %v", *sent)
	}
	clock.Advance(999 * time.Millisecond)
	if len(*sent) != 1 {
		t.Fatalf("before the window ends: %v", *sent)
	}
	clock.Advance(2 * time.Millisecond)
	if len(*sent) != 2 || (*sent)[1].session != "s1" {
		t.Fatalf("at the window's end: %v", *sent)
	}
	// What was coalesced keeps its most telling reason.
	if (*sent)[1].reason != "mcp_status" {
		t.Errorf("coalesced reason = %q", (*sent)[1].reason)
	}
	clock.Advance(10 * time.Second)
	if len(*sent) != 2 {
		t.Errorf("a quiet session sent again: %v", *sent)
	}
}

func TestContextNotifier_AfterAQuietWindowTheNextChangeIsSentAtOnce(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "usage")
	clock.Advance(3 * time.Second)
	n.changed("s1", "usage")
	if len(*sent) != 2 {
		t.Errorf("sent = %v", *sent)
	}
}

func TestContextNotifier_EachSessionCoalescesOnItsOwn(t *testing.T) {
	clock := newFakeClock()
	n, sent := notifierWith(clock)
	n.changed("s1", "usage")
	n.changed("s2", "usage")
	n.changed("s1", "usage")
	n.changed("s2", "usage")
	if len(*sent) != 2 {
		t.Fatalf("sent = %v", *sent)
	}
	clock.Advance(2 * time.Second)
	if len(*sent) != 4 {
		t.Errorf("sent = %v", *sent)
	}
}

func TestSessionContext_AChangeReachesTheProjectsFollowersAsAPlainMessage(t *testing.T) {
	svc := contextService(t)
	clock := newFakeClock()
	svc.contextNotes = newContextNotifier(clock.Now, clock.AfterFunc, svc.sendContextChange)
	svc.Contexts = memory.NewSessionContextStore()
	feed := follow(t, svc, "p1")

	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Now()})
	ch := next(t, feed)
	if ch.ContextChange == nil || ch.ContextChange.SessionID != "s1" || ch.ContextChange.Reason != "entry_added" || ch.ProjectID() != "p1" {
		t.Fatalf("change = %+v", ch)
	}
	b, err := json.Marshal(ch)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire) != 3 || wire["kind"] != "context.changed" || wire["session_id"] != "s1" || wire["reason"] != "entry_added" {
		t.Errorf("wire = %s, want exactly kind, session_id and reason", b)
	}
}

func TestSessionContext_EachKindOfChangeNamesItsReason(t *testing.T) {
	svc := contextService(t)
	clock := newFakeClock()
	svc.contextNotes = newContextNotifier(clock.Now, clock.AfterFunc, svc.sendContextChange)
	tr := &scriptedTranscript{}
	svc.TranscriptReader = tr
	feed := follow(t, svc, "p1")
	storeEntry(t, svc, serversEntry())
	if got := next(t, feed).ContextChange.Reason; got != "entry_added" {
		t.Errorf("entry: %q", got)
	}
	clock.Advance(3 * time.Second)

	if _, err := svc.applyInitReport(context.Background(), "s1", json.RawMessage(`{"mcp_servers":[{"name":"task","status":"connected"}]}`), nil, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got := next(t, feed).ContextChange.Reason; got != "mcp_status" {
		t.Errorf("init: %q", got)
	}
	clock.Advance(3 * time.Second)

	tr.add(clock.Now(), ports.TranscriptToolUse, "Bash", "x")
	if _, err := svc.SyncContextUsage(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if got := next(t, feed).ContextChange.Reason; got != "usage" {
		t.Errorf("usage: %q", got)
	}
}
