package orchestration

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestHub_PublishFanOutAndReplay(t *testing.T) {
	h := NewHub(8)
	h.Publish("s1", SessionEvent{Seq: 1, Type: "output", Text: "a"})

	ch, replay, cancel := h.Subscribe("s1")
	defer cancel()
	if len(replay) != 1 || replay[0].Text != "a" {
		t.Fatalf("replay wrong: %+v", replay)
	}

	h.Publish("s1", SessionEvent{Seq: 2, Type: "output", Text: "b"})
	select {
	case ev := <-ch:
		if ev.Text != "b" {
			t.Fatalf("want b, got %q", ev.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("no live event")
	}
}

func TestHub_PublishEphemeralNotReplayed(t *testing.T) {
	h := NewHub(8)

	// Subscriber present before the ephemeral event receives it live.
	ch, _, cancel := h.Subscribe("s1")
	defer cancel()
	h.PublishEphemeral("s1", SessionEvent{Type: "output_delta", Text: "Hi"})
	select {
	case ev := <-ch:
		if ev.Type != "output_delta" || ev.Text != "Hi" {
			t.Fatalf("live ephemeral wrong: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no live ephemeral event")
	}

	// A late joiner must NOT see it in replay.
	_, replay, cancel2 := h.Subscribe("s1")
	defer cancel2()
	for _, ev := range replay {
		if ev.Type == "output_delta" {
			t.Fatalf("ephemeral event leaked into replay: %+v", ev)
		}
	}
}

// A subscriber that falls behind is closed, never kept with a gap: it holds
// the events it took, in order, and then its channel ends. The publisher
// never waits for it.
func TestHub_SlowSubscriberIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		publish func(h *Hub, i int)
	}{
		{"persisted", func(h *Hub, i int) { h.Publish("s1", SessionEvent{Seq: int64(i), Type: "output"}) }},
		{"ephemeral", func(h *Hub, i int) {
			h.PublishEphemeral("s1", SessionEvent{Type: "output_delta", Text: strconv.Itoa(i)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHub(4)
			ch, _, cancel := h.Subscribe("s1")
			defer cancel()
			done := make(chan struct{})
			go func() {
				for i := 1; i <= 1000; i++ {
					tc.publish(h, i)
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("publishing blocked on a slow subscriber")
			}

			var got int
			for range ch {
				got++
			}
			if got == 0 || got >= 1000 {
				t.Fatalf("the slow subscriber took %d events, want its buffer's worth and then its end", got)
			}
			// Others still subscribe and get what follows.
			ch2, _, cancel2 := h.Subscribe("s1")
			defer cancel2()
			tc.publish(h, 1001)
			if _, ok := <-ch2; !ok {
				t.Fatal("a new subscriber was closed")
			}
		})
	}
}

// Ending a session closes its subscribers once they have what was published
// and forgets its replay buffer.
func TestHub_EndReleasesTheSession(t *testing.T) {
	h := NewHub(8)
	h.Publish("s1", SessionEvent{Seq: 1, Type: "output"})
	h.Publish("s2", SessionEvent{Seq: 1, Type: "output"})
	ch, _, cancel := h.Subscribe("s1")
	defer cancel()
	h.Publish("s1", SessionEvent{Seq: 2, Type: "done"})

	h.End("s1")

	if ev, ok := <-ch; !ok || ev.Seq != 2 {
		t.Fatalf("first after end = %+v, %v; want the done event still delivered", ev, ok)
	}
	if _, ok := <-ch; ok {
		t.Fatal("the subscriber was not closed when the session ended")
	}
	h.mu.Lock()
	_, subs := h.subs["s1"]
	_, replay := h.replay["s1"]
	_, other := h.replay["s2"]
	h.mu.Unlock()
	if subs || replay {
		t.Fatalf("the hub still holds the ended session: subs %v, replay %v", subs, replay)
	}
	if !other {
		t.Fatal("ending s1 forgot s2")
	}
	cancel() // cancelling after the end is harmless
}

func TestHub_CancelRemovesSubscriber(t *testing.T) {
	h := NewHub(4)
	ch, _, cancel := h.Subscribe("s1")
	cancel()
	// Publishing after cancel must not panic; channel is closed.
	h.Publish("s1", SessionEvent{Seq: 1})
	if _, ok := <-ch; ok {
		t.Fatal("expected closed channel after cancel")
	}
}

// heldBy reports whether the hub still holds anything for the session.
func heldBy(h *Hub, sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, subs := h.subs[sessionID]
	_, replay := h.replay[sessionID]
	return subs || replay
}

// drainUntilClosed reads ch until it closes and returns the events it held.
func drainUntilClosed(t *testing.T, ch <-chan SessionEvent) []SessionEvent {
	t.Helper()
	var got []SessionEvent
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("the subscriber was never closed; it got %+v", got)
		}
	}
}

// An interactive session that ends closes its followers after its last
// event, and the hub forgets it.
func TestEndInteractive_ReleasesTheSessionFromTheHub(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	ch, _, cancel := svc.Subscribe(sess.ID)
	defer cancel()

	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	got := drainUntilClosed(t, ch)
	if len(got) == 0 || got[len(got)-1].Type != "done" {
		t.Fatalf("events before the end = %+v, want the done event last", got)
	}
	if heldBy(svc.hub, sess.ID) {
		t.Fatal("the hub still holds the ended session")
	}
}

// A headless session whose agent is gone closes its followers too.
func TestStop_ReleasesTheSessionFromTheHub(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	ch, _, cancel := svc.Subscribe(d.ID)
	defer cancel()

	if err := svc.Stop(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	drainUntilClosed(t, ch)
	if heldBy(svc.hub, d.ID) {
		t.Fatal("the hub still holds the stopped session")
	}
}
