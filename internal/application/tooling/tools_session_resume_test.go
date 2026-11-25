package tooling

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// resumeWorld is a task with the calling session "me" on it, the sessions
// store to seed the ones it resumes, and the resume tool as "me" calls it.
type resumeWorld struct {
	resume    func(args map[string]any) (map[string]any, error)
	stop      func(args map[string]any) (map[string]any, error)
	starter   *fakeStarter
	stopper   *fakeStopper
	sessions  ports.SessionRepository
	taskID    string
	otherTask string
	projectID string
}

// resumeWorldWith builds the world; channel nil means the server has no
// architect channel, so every session is a peer.
func resumeWorldWith(t *testing.T, channel *fakeChannel) resumeWorld {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	task, _ := plan.CreateTicket(context.Background(), proj.ID, "Add SSE feed", "", domain.TicketStatusInProgress, "")
	other, _ := plan.CreateTicket(context.Background(), proj.ID, "Another task", "", domain.TicketStatusInProgress, "")
	if _, err := sessions.Create(&domain.Session{ID: "me", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api",
		Status: domain.SessionRunning, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{sessions: sessions}
	stopper := &fakeStopper{sessions: sessions}
	var ch ports.TaskChannel
	if channel != nil {
		ch = channel
	}
	tools := map[string]domain.Tool{}
	for _, tl := range SessionTaskTools(plan, sessions, agentList{}, nil, PeerStarter{Sessions: starter, Stopper: stopper, Repositories: repoList{{ID: "r-api", Name: "api"}}},
		ArtifactTooling{}, ch, NotificationTooling{}) {
		tools[tl.Name] = tl
	}
	call := func(name string) func(args map[string]any) (map[string]any, error) {
		tool, ok := tools[name]
		if !ok {
			t.Fatal(name + " not built")
		}
		return func(args map[string]any) (map[string]any, error) {
			out, err := tool.Handler(WithSessionID(context.Background(), "me"), args)
			if err != nil {
				return nil, err
			}
			return out.(map[string]any), nil
		}
	}
	return resumeWorld{resume: call("resume_task_session"), stop: call("stop_task_session"), starter: starter, stopper: stopper,
		sessions: sessions, taskID: task.ID, otherTask: other.ID, projectID: proj.ID}
}

// ended seeds an ended interactive session of the task that parent started.
func (w resumeWorld) ended(t *testing.T, id, parent string) {
	t.Helper()
	w.seed(t, id, parent, domain.SessionStopped)
}

// running seeds a running interactive session of the task that parent started.
func (w resumeWorld) running(t *testing.T, id, parent string) {
	t.Helper()
	w.seed(t, id, parent, domain.SessionRunning)
}

func (w resumeWorld) seed(t *testing.T, id, parent string, status domain.SessionStatus) {
	t.Helper()
	if _, err := w.sessions.Create(&domain.Session{ID: id, ProjectID: w.projectID, TicketID: w.taskID, RepositoryID: "r-api",
		Status: status, Interactive: true, Branch: "task/" + id, ParentSessionID: parent}); err != nil {
		t.Fatal(err)
	}
}

func TestResumeTaskSession_TheArchitectResumesAnEndedDelegateWithAPrompt(t *testing.T) {
	check := &domain.StatusCheck{TaskID: "t", DelegateSessionID: "d1", EveryMinutes: 10}
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect, "d1": domain.RoleDelegate}, checks: []*domain.StatusCheck{check}})
	w.ended(t, "d1", "someone-else")

	out, err := w.resume(map[string]any{"session_id": "d1", "prompt": " Pick up ticket 02. "})
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.ResumeRequest{{SessionID: "d1", RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 120, Rows: 32}, Prompt: "Pick up ticket 02."}}
	if !reflect.DeepEqual(w.starter.resumed, want) {
		t.Fatalf("resume = %+v\nwant     %+v", w.starter.resumed, want)
	}
	if out["session_id"] != "d1" || out["status"] != domain.SessionRunning || out["branch"] != "task/d1" {
		t.Errorf("out = %+v", out)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "saved conversation") || !strings.Contains(note, "list_task_sessions") {
		t.Errorf("note = %q", note)
	}
	if out["status_check"] != check {
		t.Errorf("status_check = %v, want the delegate's loop", out["status_check"])
	}
}

func TestResumeTaskSession_APeerResumesOnlyTheSessionsItStarted(t *testing.T) {
	w := resumeWorldWith(t, nil)
	w.ended(t, "mine", "me")
	w.ended(t, "theirs", "someone-else")

	if _, err := w.resume(map[string]any{"session_id": "mine"}); err != nil {
		t.Fatalf("resume of a session it started: %v", err)
	}
	if got := w.starter.resumed[0].Prompt; got != "" {
		t.Errorf("prompt = %q, want none", got)
	}
	_, err := w.resume(map[string]any{"session_id": "theirs"})
	if errs.Code(err) != "RESUME_NOT_ALLOWED" || !strings.Contains(err.Error(), "theirs") {
		t.Fatalf("resume of another's session = %v, want RESUME_NOT_ALLOWED naming it", err)
	}
	if len(w.starter.resumed) != 1 {
		t.Errorf("%d resumes, want only the allowed one", len(w.starter.resumed))
	}
}

func TestResumeTaskSession_ADelegateIsAPeerToo(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleDelegate}})
	w.ended(t, "theirs", "someone-else")
	if _, err := w.resume(map[string]any{"session_id": "theirs"}); errs.Code(err) != "RESUME_NOT_ALLOWED" {
		t.Fatalf("a delegate resuming a session it did not start = %v, want RESUME_NOT_ALLOWED", err)
	}
}

func TestResumeTaskSession_RefusesWhatIsNotAnEndedSessionOfThisTask(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect}})
	if _, err := w.sessions.Create(&domain.Session{ID: "elsewhere", ProjectID: w.projectID, TicketID: w.otherTask, Status: domain.SessionStopped, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, id, code string
	}{
		{"no id", " ", "INVALID_INPUT"},
		{"unknown", "nobody", "SESSION_NOT_FOUND"},
		{"another task", "elsewhere", "SESSION_NOT_ON_TASK"},
		{"itself", "me", "INVALID_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.resume(map[string]any{"session_id": tc.id})
			if errs.Code(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
	if len(w.starter.resumed) != 0 {
		t.Errorf("%d resumes, want none", len(w.starter.resumed))
	}
}

// Orchestration's own refusals (running, not interactive, a conversation
// that cannot be resumed) come back with their codes and the session named.
func TestResumeTaskSession_OrchestrationsRefusalKeepsItsCode(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect}})
	w.ended(t, "d1", "")
	w.starter.resumeErr = &domain.StructuredError{Code: "SESSION_ALREADY_RUNNING", Message: "session is still running"}
	_, err := w.resume(map[string]any{"session_id": "d1"})
	if errs.Code(err) != "SESSION_ALREADY_RUNNING" || !strings.Contains(err.Error(), "d1") {
		t.Fatalf("err = %v, want SESSION_ALREADY_RUNNING naming d1", err)
	}
}

func TestResumeTaskSession_IsHeldToTheTasksSessionLimit(t *testing.T) {
	w := resumeWorldWith(t, &fakeChannel{roles: map[string]domain.SessionRole{"me": domain.RoleArchitect}})
	w.ended(t, "d1", "")
	for i := 1; i < MaxLiveTaskSessions; i++ { // "me" is the first live one
		if _, err := w.sessions.Create(&domain.Session{ID: "live-" + strconv.Itoa(i), ProjectID: w.projectID, TicketID: w.taskID, Status: domain.SessionRunning}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.resume(map[string]any{"session_id": "d1"}); errs.Code(err) != "TASK_SESSION_LIMIT" {
		t.Fatalf("err = %v, want TASK_SESSION_LIMIT", err)
	}
	// A running session is refused for running, not for the limit.
	if _, err := w.resume(map[string]any{"session_id": "live-1"}); errs.Code(err) != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("resume of a running session at the limit = %v, want SESSION_ALREADY_RUNNING", err)
	}
	if len(w.starter.resumed) != 0 {
		t.Error("a resume over the limit reached orchestration")
	}
}
