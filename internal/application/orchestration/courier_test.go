//go:build !windows

package orchestration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An idle server-hosted session has the turn typed into its terminal; a busy
// one gets it from its Stop hook, and goes on with it.
func TestCourier_HostedSession(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: "cat"})
	sess := startOnServer(t, svc)
	var first, second atomic.Int32

	now, err := svc.Deliver(sess.ID, "msg:1", "[task message 1 · question · from a session x]\nhello there", func(time.Time) { first.Add(1) })
	if err != nil || !now || first.Load() != 1 {
		t.Fatalf("an idle session takes it at once: %v %v", now, err)
	}
	screenOf(t, svc, sess.ID, "hello there")

	now, _ = svc.Deliver(sess.ID, "check:d", "[status check · old]", func(time.Time) { second.Add(1) })
	_, _ = svc.Deliver(sess.ID, "check:d", "[status check · new]", func(time.Time) { second.Add(1) })
	if now || second.Load() != 0 {
		t.Fatal("the typed turn is running: the next one waits")
	}
	next, err := svc.TurnEnded(context.Background(), sess.ID, false)
	if err != nil || next != "[status check · new]" || second.Load() != 1 {
		t.Fatalf("the Stop hook hands back the newest waiting turn, once: %q %v %d", next, err, second.Load())
	}
	if next, _ := svc.TurnEnded(context.Background(), sess.ID, true); next != "" {
		t.Fatalf("nothing left: the session stops, %q", next)
	}
	if svc.courier.turn(sess.ID) != turnIdle {
		t.Fatal("idle after a Stop with nothing waiting")
	}

	// A person typing keeps the courier out of the terminal.
	term, err := svc.AttachTerminal(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = term.Key(ports.KeyEvent{Text: "x"})
	if now, _ := svc.Deliver(sess.ID, "msg:2", "later", nil); now {
		t.Fatal("not while a person is typing")
	}

	if err := svc.TurnStarted(context.Background(), sess.ID); err != nil || svc.courier.turn(sess.ID) != turnBusy {
		t.Fatalf("UserPromptSubmit marks it busy: %v", err)
	}
}

// A client-run session can only take turns from its Stop hook; a session
// started with a prompt is busy until its first Stop.
func TestCourier_ClientSessionWaitsForStop(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	if svc.courier.turn(sess.ID) != turnBusy {
		t.Fatal("started with a prompt: busy")
	}
	var got atomic.Int32
	if now, err := svc.Deliver(sess.ID, "msg:1", "hi", func(time.Time) { got.Add(1) }); now || err != nil {
		t.Fatalf("queued: %v %v", now, err)
	}
	if next, _ := svc.TurnEnded(context.Background(), sess.ID, false); next != "hi" || got.Load() != 1 {
		t.Fatalf("delivered by the Stop hook: %q", next)
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deliver(sess.ID, "msg:2", "hi", nil); codeOf(err) != "SESSION_NOT_RUNNING" {
		t.Fatalf("an ended session takes nothing: %v", err)
	}
}

// A headless session takes a turn through Send when idle, and what waits
// goes in when its turn ends.
func TestCourier_HeadlessSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, svc, d.ID, domain.SessionIdle)
	var n atomic.Int32
	for i, text := range []string{"one", "two", "three"} {
		if _, err := svc.Deliver(d.ID, "msg:"+string(rune('a'+i)), text, func(time.Time) { n.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "every turn delivered", func() bool { return n.Load() == 3 })
	waitForStatus(t, svc, d.ID, domain.SessionIdle)
}

// The orchestration announces sessions starting and ending, and refuses a
// status-check interval out of range before anything is created.
func TestLifecycleEvents(t *testing.T) {
	svc, _ := newInteractiveService(t)
	bus := eventbus.New()
	svc.Events = bus
	var started []domain.SessionStarted
	var ended []domain.SessionEnded
	ports.On(bus, func(_ context.Context, ev domain.SessionStarted) error { started = append(started, ev); return nil })
	ports.On(bus, func(_ context.Context, ev domain.SessionEnded) error { ended = append(ended, ev); return nil })

	five := 5
	sess, _ := startInteractive(t, svc, InteractiveRequest{ParentSessionID: "arch", StatusCheckMinutes: &five})
	if len(started) != 1 || started[0].SessionID != sess.ID || started[0].ParentSessionID != "arch" || *started[0].StatusCheckMinutes != 5 || started[0].TicketID != "tk1" {
		t.Fatalf("started: %+v", started)
	}
	one := 1
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", StatusCheckMinutes: &one})
	if codeOf(err) != "INVALID_INPUT" || len(started) != 1 {
		t.Fatalf("out of range is refused before the start: %v", err)
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(ended) != 1 || ended[0].Status != domain.SessionFailed {
		t.Fatalf("ended: %+v", ended)
	}
}
