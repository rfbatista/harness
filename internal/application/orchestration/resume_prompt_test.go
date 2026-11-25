//go:build !windows

package orchestration

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// stoppedIdle is a session that ended between turns: a resume of it is not
// interrupted.
func stoppedIdle(t *testing.T, svc *Service, id string) {
	t.Helper()
	orphan(t, svc, id, domain.SessionIdle)
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// A resume that carries a prompt starts the conversation again on it, so the
// agent answers as soon as it is back.
func TestResume_WithAPromptStartsOnThatPrompt(t *testing.T) {
	svc, _ := newInteractiveService(t)
	stoppedIdle(t, svc, "s1")

	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1", Prompt: "Pick up ticket 02."})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Prompt != "Pick up ticket 02." {
		t.Errorf("first turn = %q, want the prompt", spec.Prompt)
	}
	if !spec.Conversation.Resume {
		t.Error("the resume did not continue the saved conversation")
	}
	if got := svc.courier.turn("s1"); got != turnBusy {
		t.Errorf("turn = %v, want busy: the prompt is its first turn", got)
	}
}

// An interrupted session is told of the restart first, and then gets the
// prompt, in one first turn.
func TestResume_OfAnInterruptedSessionWithAPromptSendsTheContinueTurnThenThePrompt(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.BranchRequests = pendingRequests{}
	orphan(t, svc, "s1", domain.SessionThinking)
	if _, err := svc.StopOrphanedServerSessions(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1", Prompt: "Pick up ticket 02."})
	if err != nil {
		t.Fatal(err)
	}
	restart, prompt := strings.Index(spec.Prompt, "restarted"), strings.Index(spec.Prompt, "Pick up ticket 02.")
	if restart < 0 || prompt < 0 || restart > prompt {
		t.Errorf("first turn = %q, want the continue turn and then the prompt", spec.Prompt)
	}
	if got := svc.courier.turn("s1"); got != turnBusy {
		t.Errorf("turn = %v, want busy", got)
	}
}

// Without a prompt an unmarked session still resumes idle.
func TestResume_WithoutAPromptStaysIdle(t *testing.T) {
	svc, _ := newInteractiveService(t)
	stoppedIdle(t, svc, "s1")

	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Prompt != "" {
		t.Errorf("first turn = %q, want none", spec.Prompt)
	}
	if got := svc.courier.turn("s1"); got != turnIdle {
		t.Errorf("turn = %v, want idle", got)
	}
}

// A resume is announced, so the architect channel can bring back the loop
// that ended with the session.
func TestResume_IsAnnounced(t *testing.T) {
	svc, _ := newInteractiveService(t)
	bus := eventbus.New()
	svc.Events = bus
	var resumed []domain.SessionResumed
	ports.On(bus, func(_ context.Context, ev domain.SessionResumed) error { resumed = append(resumed, ev); return nil })
	stoppedIdle(t, svc, "s1")

	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if len(resumed) != 1 || resumed[0].SessionID != "s1" || resumed[0].TicketID != "tk1" || resumed[0].ProjectID != "p1" {
		t.Fatalf("resumed: %+v", resumed)
	}
}
