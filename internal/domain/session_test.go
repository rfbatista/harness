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
	for in, want := range map[string]SessionMode{"": SessionModeDefault, "architect": SessionModeArchitect, "implement": SessionModeImplement, "design": SessionModeDesign, "product": SessionModeProduct} {
		got, err := ParseSessionMode(in)
		if err != nil || got != want {
			t.Errorf("ParseSessionMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParseSessionMode("wizard")
	if err == nil || !strings.Contains(err.Error(), `"design"`) || !strings.Contains(err.Error(), `"implement"`) || !strings.Contains(err.Error(), `"product"`) {
		t.Fatalf("unknown mode must be refused and the message must list design, implement and product: %v", err)
	}
}

// Product is the only mode that runs without a task: every other mode,
// the default included, needs one.
func TestSessionMode_NeedsTask(t *testing.T) {
	for mode, want := range map[SessionMode]bool{
		SessionModeDefault: true, SessionModeArchitect: true, SessionModeImplement: true, SessionModeDesign: true,
		SessionModeProduct: false,
	} {
		if got := mode.NeedsTask(); got != want {
			t.Errorf("SessionMode(%q).NeedsTask() = %v, want %v", mode, got, want)
		}
	}
}
