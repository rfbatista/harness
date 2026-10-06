package domain

import (
	"strings"
	"testing"
)

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

func TestParseSessionMode(t *testing.T) {
	for in, want := range map[string]SessionMode{"": SessionModeDefault, "architect": SessionModeArchitect, "design": SessionModeDesign} {
		got, err := ParseSessionMode(in)
		if err != nil || got != want {
			t.Errorf("ParseSessionMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParseSessionMode("wizard")
	if err == nil || !strings.Contains(err.Error(), `"design"`) {
		t.Fatalf("unknown mode must be refused and the message must list design: %v", err)
	}
}
