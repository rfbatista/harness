package tooling

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeStopper records what it was asked to stop and marks the session
// stopped, as orchestration would.
type fakeStopper struct {
	sessions ports.SessionRepository
	got      []string
	err      error // refuses every stop
}

func (f *fakeStopper) Stop(_ context.Context, id string) error {
	f.got = append(f.got, id)
	if f.err != nil {
		return f.err
	}
	return f.sessions.UpdateStatus(id, domain.SessionStopped)
}

func TestStopTaskSession_TheArchitectStopsARunningDelegate(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect, "d1": domain.RoleDelegate}})
	w.running(t, "d1", "someone-else")

	out, err := w.stop(map[string]any{"session_id": " d1 "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.stopper.got, []string{"d1"}) {
		t.Fatalf("stopped %q, want d1", w.stopper.got)
	}
	if out["session_id"] != "d1" || out["status"] != domain.SessionStopped {
		t.Errorf("out = %+v", out)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "resume_task_session") {
		t.Errorf("note = %q, want it to say how to bring it back", note)
	}
}

func TestStopTaskSession_APeerStopsOnlyTheSessionsItStarted(t *testing.T) {
	w := resumeWorldWith(t, nil)
	w.running(t, "mine", "me")
	w.running(t, "theirs", "someone-else")

	if _, err := w.stop(map[string]any{"session_id": "mine"}); err != nil {
		t.Fatalf("stop of a session it started: %v", err)
	}
	_, err := w.stop(map[string]any{"session_id": "theirs"})
	if errs.Code(err) != "STOP_NOT_ALLOWED" || !strings.Contains(err.Error(), "theirs") {
		t.Fatalf("stop of another's session = %v, want STOP_NOT_ALLOWED naming it", err)
	}
	if !reflect.DeepEqual(w.stopper.got, []string{"mine"}) {
		t.Errorf("stopped %q, want only the allowed one", w.stopper.got)
	}
}

func TestStopTaskSession_RefusesWhatIsNotARunningSessionOfThisTask(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect}})
	w.ended(t, "done", "")
	if _, err := w.sessions.Create(&domain.Session{ID: "elsewhere", ProjectID: w.projectID, TicketID: w.otherTask, Status: domain.SessionRunning, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, id, code string
	}{
		{"no id", " ", "INVALID_INPUT"},
		{"unknown", "nobody", "SESSION_NOT_FOUND"},
		{"another task", "elsewhere", "SESSION_NOT_ON_TASK"},
		{"itself", "me", "INVALID_INPUT"},
		{"ended", "done", "SESSION_NOT_RUNNING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.stop(map[string]any{"session_id": tc.id})
			if errs.Code(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
	if len(w.stopper.got) != 0 {
		t.Errorf("stopped %q, want none", w.stopper.got)
	}
}

func TestStopTaskSession_OrchestrationsRefusalKeepsItsCode(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect}})
	w.running(t, "d1", "")
	w.stopper.err = &domain.StructuredError{Code: "SESSION_RUNS_ON_TUI", Message: "this session runs in the client that started it; close it there"}
	_, err := w.stop(map[string]any{"session_id": "d1"})
	if errs.Code(err) != "SESSION_RUNS_ON_TUI" || !strings.Contains(err.Error(), "d1") {
		t.Fatalf("err = %v, want SESSION_RUNS_ON_TUI naming d1", err)
	}
}
