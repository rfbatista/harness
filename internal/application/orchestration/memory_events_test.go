//go:build !windows

package orchestration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func sampleOf(sess *domain.Session, at time.Time, rss int64) domain.MemorySample {
	return domain.MemorySample{Kind: domain.MemorySampleKind, SessionID: sess.ID, TaskID: sess.TicketID, SampledAt: at, RSSBytes: rss,
		Budget: 4 * gb, Processes: 7, Heaviest: &domain.MemoryProcess{PID: 81234, Command: "node jest", RSSBytes: 905969664}}
}

func jsonMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func recvEvent(t *testing.T, ch <-chan SessionEvent) SessionEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no session event")
	}
	return SessionEvent{}
}

// A sample goes to the session's stream as the contract says, live only: a
// subscriber that joins later is not replayed it.
func TestAMemorySampleReachesTheSessionStreamLiveOnlyInTheContractsShape(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	live, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	at := time.Date(2026, 11, 25, 22, 40, 1, 0, time.UTC)
	svc.Sampled(sampleOf(sess, at, 1288490188))
	ev := recvEvent(t, live)
	m := jsonMap(t, ev)
	if m["kind"] != "session.memory" || m["session_id"] != sess.ID || m["task_id"] != sess.TicketID || m["sampled_at"] != "2026-11-25T22:40:01Z" ||
		m["rss_bytes"] != float64(1288490188) || m["budget_bytes"] != float64(4*gb) || m["processes"] != float64(7) {
		t.Errorf("event = %v", m)
	}
	if h, _ := m["heaviest"].(map[string]any); h["pid"] != float64(81234) || h["command"] != "node jest" || h["rss_bytes"] != float64(905969664) {
		t.Errorf("heaviest = %v", m["heaviest"])
	}

	_, replay, cancel2 := svc.Subscribe(sess.ID)
	defer cancel2()
	for _, r := range replay {
		if r.Type == "session.memory" {
			t.Errorf("a late subscriber was replayed a sample: %+v", r)
		}
	}
}

// A kill is durable: live, replayed to a late subscriber, and on the project feed, never throttled.
func TestAMemoryKillIsDurableAndReachesTheSessionStreamAndTheProjectFeed(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	feed := follow(t, svc, "p1")
	live, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	k := killOf(sess.ID)
	k.TaskID = sess.TicketID
	svc.Killed(k)
	svc.Killed(k) // never throttled

	m := jsonMap(t, recvEvent(t, live))
	if m["kind"] != "session.memory_killed" || m["pid"] != float64(81234) || m["command"] != "node jest" || m["signal"] != "SIGTERM" ||
		m["tree_rss_bytes"] != float64(4200<<20) || m["rss_bytes"] != float64(3584<<20) || m["budget_bytes"] != float64(4*gb) || m["killed_at"] == nil {
		t.Errorf("kill event = %v", m)
	}
	for range 2 {
		c := next(t, feed)
		if c.MemoryKilled == nil || c.MemoryKilled.PID != 81234 || c.ProjectID() != "p1" {
			t.Fatalf("feed change = %+v, want the kill on project p1", c)
		}
		if raw := jsonMap(t, c); raw["memory_killed"] == nil || raw["session"] != nil {
			t.Errorf("wire = %v", raw)
		}
	}
	_, replay, cancel2 := svc.Subscribe(sess.ID)
	defer cancel2()
	n := 0
	for _, r := range replay {
		if r.Type == "session.memory_killed" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("replayed kills = %d, want 2", n)
	}
}

// The project feed takes a sample at most every 5 s per session, or sooner when
// total RSS moved by more than 5 % since the last one it published.
func TestTheProjectFeedThrottlesSamplesToFiveSecondsOrFivePercent(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	feed := follow(t, svc, "p1")
	t0 := time.Date(2026, 11, 25, 22, 40, 0, 0, time.UTC)

	var got []int64
	collect := func() {
		for {
			select {
			case c := <-feed:
				if c.Memory != nil {
					got = append(got, c.Memory.RSSBytes)
				}
			case <-time.After(50 * time.Millisecond):
				return
			}
		}
	}
	steady := 1000 * mb
	for i := range 12 {
		svc.Sampled(sampleOf(sess, t0.Add(time.Duration(i)*time.Second), steady+int64(i)*mb)) // under 5 %: wait for 5 s
	}
	collect()
	if len(got) != 3 { // t=0, t=5, t=10
		t.Errorf("published = %v, want the first sample and one every 5 s", got)
	}
	got = nil
	svc.Sampled(sampleOf(sess, t0.Add(12*time.Second), 1500*mb)) // +50 %: at once
	collect()
	if len(got) != 1 || got[0] != 1500*mb {
		t.Errorf("a jump of more than 5 %% was not published at once: %v", got)
	}
}

const mb = int64(1) << 20

// When the session ends, the last sample is empty so consumers can clear it, and the initial read stops.
func TestTheLastSampleOfAnEndedSessionIsEmptyAndTheInitialReadSaysNotLive(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	svc.Sampled(sampleOf(sess, time.Now(), 800*mb))
	k := killOf(sess.ID)
	svc.Killed(k)

	read, live := svc.SessionMemory(sess.ID)
	if !live || read.RSSBytes != 800*mb || len(read.Kills) != 1 || read.Kills[0].Command != "node jest" || read.SampledAt == nil {
		t.Fatalf("initial read = %+v live %v", read, live)
	}
	if got := svc.withResumability(context.Background(), svc.sessions.Get(sess.ID)); got.Memory == nil || got.Memory.RSSBytes != 800*mb {
		t.Errorf("the session payload's memory = %+v, want the last sample", got.Memory)
	}

	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, live := svc.SessionMemory(sess.ID); live {
		t.Error("an ended session reads as live")
	}
	var _ ports.MemorySink = svc
}

// The session list (what the whiteboard loads) carries the last sample of each
// live session at once, not only after the next throttled feed change; a
// session that is not live has none.
func TestTheSessionListCarriesTheLastSampleOfEachLiveSessionAndNoneForAnEndedOne(t *testing.T) {
	svc, _ := newInteractiveService(t)
	live, _ := startInteractive(t, svc, InteractiveRequest{})
	ended, _ := startInteractive(t, svc, InteractiveRequest{})
	svc.Sampled(sampleOf(live, time.Now(), 700*mb))
	svc.Sampled(sampleOf(ended, time.Now(), 300*mb))
	if _, err := svc.EndInteractive(context.Background(), ended.ID, 0, true); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(context.Background(), ports.SessionFilter{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*domain.Session{}
	for _, s := range list {
		byID[s.ID] = s
	}
	if m := byID[live.ID].Memory; m == nil || m.RSSBytes != 700*mb {
		t.Errorf("the live session's memory = %+v, want the last sample", m)
	}
	if byID[ended.ID].Memory != nil {
		t.Errorf("the ended session's memory = %+v, want null", byID[ended.ID].Memory)
	}
	raw := jsonMap(t, byID[ended.ID])
	if v, present := raw["memory"]; !present || v != nil {
		t.Errorf("an ended session's wire memory = %v (present %v), want an explicit null", v, present)
	}
}
