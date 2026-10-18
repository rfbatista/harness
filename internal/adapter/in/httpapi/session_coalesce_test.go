package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// publishDeltas publishes n output deltas on s1, numbered from first, at the
// pace a session's pump does: bursts the follower drains as they come, never
// all at once. It returns their text, concatenated in order.
func (o *hubOrch) publishDeltas(first, n int) string {
	var all strings.Builder
	for i := first; i < first+n; i++ {
		text := fmt.Sprintf("delta %06d;", i)
		all.WriteString(text)
		o.hub.PublishEphemeral("s1", ports.SessionEvent{SessionID: "s1", Type: "output_delta", DeltaKind: "text", Text: text})
		if i%16 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	return all.String()
}

// record stores and publishes a persisted event on s1.
func (o *hubOrch) record(ev ports.SessionEvent) {
	o.mu.Lock()
	o.log = append(o.log, ev)
	o.mu.Unlock()
	o.hub.Publish("s1", ev)
}

// A burst of deltas reaches a follower that has not read it yet as fewer
// events whose output, concatenated, is the burst's, in order. A persisted
// event in the middle of the burst is neither merged nor moved, and keeps its
// seq.
func TestFollowSession_MergesABurstOfDeltasAroundPersistedEvents(t *testing.T) {
	orch := newHubOrch()
	h := &Handler{orchSvc: orch}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := h.followSession(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}

	const burst = 1000
	before := orch.publishDeltas(0, burst)
	orch.record(ports.SessionEvent{Seq: 1, SessionID: "s1", Type: "output", Text: "persisted"})
	after := orch.publishDeltas(burst, burst)

	var events []ports.SessionEvent
	var text [2]strings.Builder
	part := 0
	for text[0].Len()+text[1].Len() < len(before)+len(after) {
		select {
		case v, ok := <-out:
			if !ok {
				t.Fatalf("the feed ended after %d events: the follower fell behind", len(events))
			}
			ev := v.(ports.SessionEvent)
			events = append(events, ev)
			switch {
			case ev.Type == "output_delta" && ev.Seq == 0:
				text[part].WriteString(ev.Text)
			case ev.Seq == 1 && ev.Text == "persisted" && part == 0:
				part = 1
			default:
				t.Fatalf("event %d = %+v, want a delta or the persisted event once", len(events), ev)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("got %d events, then nothing", len(events))
		}
	}
	if part != 1 {
		t.Fatal("the persisted event never arrived")
	}
	if text[0].String() != before || text[1].String() != after {
		t.Fatal("the deltas' output, concatenated, is not the burst's in order around the persisted event")
	}
	if len(events) >= 2*burst {
		t.Fatalf("%d events for %d deltas: nothing was merged", len(events), 2*burst)
	}
}

// Deltas of different content blocks, or of a different kind, are not merged:
// each event carries the output of one block, under its own kind and index.
func TestFollowSession_MergesOnlyDeltasOfOneBlock(t *testing.T) {
	orch := newHubOrch()
	h := &Handler{orchSvc: orch}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := h.followSession(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, ev := range []ports.SessionEvent{
		{Type: "output_delta", DeltaKind: "thinking", Index: 0, Text: "a"},
		{Type: "output_delta", DeltaKind: "thinking", Index: 0, Text: "b"},
		{Type: "output_delta", DeltaKind: "text", Index: 1, Text: "c"},
		{Type: "output_delta", DeltaKind: "text", Index: 1, Text: "d"},
		{Type: "output_delta", DeltaKind: "text", Index: 2, Text: "e"},
	} {
		ev.SessionID = "s1"
		orch.hub.PublishEphemeral("s1", ev)
	}

	// The first delta may be taken before the next arrives; what follows it
	// merges while it waits. Each block's output is gathered under its name.
	var blocks []string
	var texts []string
	for n := 0; n < len("abcde"); {
		select {
		case v := <-out:
			ev := v.(ports.SessionEvent)
			block := fmt.Sprintf("%s%d", ev.DeltaKind, ev.Index)
			if len(blocks) == 0 || blocks[len(blocks)-1] != block {
				blocks = append(blocks, block)
				texts = append(texts, "")
			}
			texts[len(texts)-1] += ev.Text
			n += len(ev.Text)
		case <-time.After(5 * time.Second):
			t.Fatalf("got %v %v, then nothing", blocks, texts)
		}
	}
	got := fmt.Sprint(blocks, texts)
	if got != "[thinking0 text1 text2] [ab cd e]" {
		t.Fatalf("blocks and their output = %s, want [thinking0 text1 text2] [ab cd e]", got)
	}
}

const burstPastTheQueue = 20000

// A burst of deltas many times the follow queue does not end the session feed
// of a tab that reads its stream at its own pace; it gets all the output.
func TestStream_ABurstOfDeltasPastTheQueueDoesNotEndTheSessionFeed(t *testing.T) {
	orch := newHubOrch()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: orch}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	s := openStream(t, srv)

	const path = "/sessions/s1/events?from_seq=0"
	if code, body := s.post("follow", path); code != http.StatusOK {
		t.Fatalf("follow = %d %v", code, body)
	}
	want := orch.publishDeltas(0, burstPastTheQueue)

	var got strings.Builder
	events := 0
	for got.Len() < len(want) {
		m := s.next()
		if m["path"] != path || m["end"] == true {
			t.Fatalf("after %d events: message = %v, want more output on %s", events, m, path)
		}
		got.WriteString(m["data"].(map[string]any)["text"].(string))
		events++
	}
	if got.String() != want {
		t.Fatal("the output, concatenated, is not the burst's in order")
	}
	if events >= burstPastTheQueue {
		t.Fatalf("%d events for %d deltas: nothing was merged", events, burstPastTheQueue)
	}
}

// The same burst does not close the per-feed session SSE endpoint either.
func TestSessionEvents_ABurstOfDeltasPastTheBufferDoesNotCloseTheFeed(t *testing.T) {
	orch := newHubOrch()
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: orch}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	resp := openSessionSSE(t, srv, "")
	want := orch.publishDeltas(0, burstPastTheQueue)

	texts := make(chan string, 16)
	go func() {
		defer close(texts)
		scan := bufio.NewScanner(resp.Body)
		scan.Buffer(make([]byte, 64*1024), 1024*1024)
		for scan.Scan() {
			if data, ok := strings.CutPrefix(scan.Text(), "data: "); ok {
				var ev ports.SessionEvent
				_ = json.Unmarshal([]byte(data), &ev)
				texts <- ev.Text
			}
		}
	}()
	var got strings.Builder
	for got.Len() < len(want) {
		select {
		case text, ok := <-texts:
			if !ok {
				t.Fatalf("the connection closed after %d of %d bytes of output", got.Len(), len(want))
			}
			got.WriteString(text)
		case <-time.After(5 * time.Second):
			t.Fatalf("after %d of %d bytes the feed sent nothing more", got.Len(), len(want))
		}
	}
	if got.String() != want {
		t.Fatal("the output, concatenated, is not the burst's in order")
	}
}
