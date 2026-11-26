//go:build !windows

package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func killOf(sessionID string) domain.MemoryKill {
	return domain.MemoryKill{Kind: domain.MemoryKilledKind, SessionID: sessionID, PID: 81234, Command: "node jest",
		RSSBytes: 3584 << 20, TreeRSS: 4200 << 20, Budget: 4 << 30, Signal: "SIGTERM", KilledAt: time.Now()}
}

// An ended process that belonged to no running call reaches the agent through
// the courier when its turn ends, once.
func TestAnEncerramentoOutsideAnyCallReachesTheAgentThroughTheCourier(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})

	svc.Killed(killOf(sess.ID))
	text, err := svc.TurnEnded(context.Background(), sess.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"node jest", "3.5 GB", "4 GB", "fewer workers"} {
		if !strings.Contains(text, want) {
			t.Errorf("the delivered turn lacks %q:\n%s", want, text)
		}
	}
	if again, _ := svc.TurnEnded(context.Background(), sess.ID, true); again != "" {
		t.Errorf("delivered twice: %q", again)
	}
}

// An ended process while a Bash call runs is told in that call's PostToolUse
// answer, and not again by the courier.
func TestAnEncerramentoDuringABashCallIsToldInThatCallsAnswerAndNotTwice(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})

	svc.BashStarted(sess.ID, "call-1")
	svc.Killed(killOf(sess.ID))
	if got := svc.BashEnded(sess.ID, "call-2"); got != "" {
		t.Errorf("another call's answer carries %q", got)
	}
	got := svc.BashEnded(sess.ID, "call-1")
	for _, want := range []string{"node jest", "3.5 GB", "4 GB", "fewer workers"} {
		if !strings.Contains(got, want) {
			t.Errorf("the PostToolUse reason lacks %q:\n%s", want, got)
		}
	}
	if again := svc.BashEnded(sess.ID, "call-1"); again != "" {
		t.Errorf("told twice: %q", again)
	}
	if text, _ := svc.TurnEnded(context.Background(), sess.ID, true); text != "" {
		t.Errorf("the courier repeated it: %q", text)
	}
}

// If the call never answers, the turn's end delivers the reason after all.
func TestAnEncerramentoDuringACallThatNeverAnswersIsDeliveredAtTurnEnd(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	svc.BashStarted(sess.ID, "call-1")
	svc.Killed(killOf(sess.ID))
	text, _ := svc.TurnEnded(context.Background(), sess.ID, false)
	if !strings.Contains(text, "node jest") {
		t.Errorf("turn end delivered %q, want the reason", text)
	}
}
