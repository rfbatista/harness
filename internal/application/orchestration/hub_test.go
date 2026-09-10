package orchestration

import (
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

func TestHub_SlowSubscriberDropped(t *testing.T) {
	h := NewHub(4)
	ch, _, cancel := h.Subscribe("s1")
	defer cancel()
	// Overflow the per-subscriber buffer; Publish must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish("s1", SessionEvent{Seq: int64(i), Type: "output"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
	_ = ch
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
