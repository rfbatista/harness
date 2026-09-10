package backend

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

func TestFakeSubscribeReplaysThenStreams(t *testing.T) {
	f := NewFake()
	f.Sessions = []*domain.Session{{ID: "s1", Status: domain.SessionRunning}}
	f.Emit("s1", orchestration.SessionEvent{Type: "user_message", Text: "hi"})
	ch, replay, cancel := f.Subscribe("s1")
	defer cancel()
	if len(replay) != 1 || replay[0].Seq != 1 || replay[0].Text != "hi" {
		t.Fatalf("replay: %+v", replay)
	}
	f.Emit("s1", orchestration.SessionEvent{Type: "output", Text: "hello"})
	ev := <-ch
	if ev.Seq != 2 || ev.Text != "hello" || ev.SessionID != "s1" {
		t.Fatalf("streamed: %+v", ev)
	}
	if h := f.History("s1", 1); len(h) != 1 || h[0].Seq != 2 {
		t.Fatalf("history from seq 1: %+v", h)
	}
}

func TestFakeEmitDeltaIsEphemeral(t *testing.T) {
	f := NewFake()
	f.Sessions = []*domain.Session{{ID: "s1"}}
	ch, _, cancel := f.Subscribe("s1")
	defer cancel()
	f.EmitDelta("s1", orchestration.SessionEvent{Type: "output_delta", Text: "he", DeltaKind: "text"})
	ev := <-ch
	if ev.Seq != 0 || len(f.History("s1", 0)) != 0 {
		t.Fatalf("delta should stream with seq 0 and never persist: %+v", ev)
	}
}

func TestFakeSendResolveAnswerAutoRunStopDelete(t *testing.T) {
	f := NewFake()
	f.Sessions = []*domain.Session{{ID: "s1", Status: domain.SessionIdle}}
	ctx := context.Background()
	if err := f.Send(ctx, "s1", "do more"); err != nil || len(f.Sent) != 1 || f.Sent[0].Text != "do more" {
		t.Fatalf("send: %v %+v", err, f.Sent)
	}
	if f.Sessions[0].Status != domain.SessionThinking {
		t.Fatalf("send should flip status to thinking: %v", f.Sessions[0].Status)
	}
	if err := f.Send(ctx, "nope", "x"); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Fatalf("missing session: %v", err)
	}
	if err := f.Resolve(ctx, "s1", "req1", true, ""); err != nil || len(f.Decisions) != 1 || !f.Decisions[0].Allow {
		t.Fatalf("resolve: %v %+v", err, f.Decisions)
	}
	if err := f.Answer(ctx, "s1", "req2", map[string]string{"Which?": "A"}, nil); err != nil || len(f.Decisions) != 2 || f.Decisions[1].Answers["Which?"] != "A" {
		t.Fatalf("answer: %v %+v", err, f.Decisions)
	}
	if err := f.Answer(ctx, "s1", "req3", nil, nil); errs.Code(err) != "NO_ANSWERS" {
		t.Fatalf("empty answers: %v", err)
	}
	if err := f.SetAutoRun(ctx, "s1", true); err != nil || !f.Sessions[0].AutoRun {
		t.Fatalf("auto-run: %v", err)
	}
	if err := f.Stop(ctx, "s1"); err != nil || f.Sessions[0].Status != domain.SessionStopped {
		t.Fatalf("stop: %v", err)
	}
	if err := f.DeleteSession(ctx, "s1"); err != nil || len(f.Sessions) != 0 {
		t.Fatalf("delete: %v", err)
	}
	if got := f.GetSession("s1"); got != nil {
		t.Fatal("deleted session should be gone")
	}
}
