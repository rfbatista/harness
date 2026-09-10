package domain

import "testing"

func TestSessionStatusValues(t *testing.T) {
	cases := []SessionStatus{
		SessionStarting, SessionRunning, SessionIdle, SessionThinking, SessionWaitingApproval,
		SessionPaused, SessionDone, SessionFailed, SessionStopped,
	}
	if len(cases) != 9 {
		t.Fatal("expected 9 statuses")
	}
	if SessionWaitingApproval != "waiting_approval" {
		t.Fatalf("unexpected value %q", SessionWaitingApproval)
	}
	if SessionIdle != "idle" {
		t.Fatalf("unexpected value %q", SessionIdle)
	}
}
