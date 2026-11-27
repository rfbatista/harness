package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type recordingTasks map[string]*domain.Ticket

func (r recordingTasks) Get(_ context.Context, id string) *domain.Ticket { return r[id] }

// recordedSession is one ended session on task t1 with a tool call.
type recordedSession struct{}

func (recordedSession) Find(_ context.Context, id string) (*domain.Session, error) {
	return &domain.Session{ID: id, ProjectID: "p1", RepositoryID: "r1", TicketID: "t1", Task: "Read the README"}, nil
}

func (recordedSession) ListEvents(string, int64) ([]ports.StoredEvent, error) {
	ev := func(seq int64, e ports.SessionEvent) ports.StoredEvent {
		b, _ := json.Marshal(e)
		return ports.StoredEvent{Seq: seq, Type: e.Type, Payload: b}
	}
	return []ports.StoredEvent{
		ev(1, ports.SessionEvent{Type: "tool_use", ToolName: "read_file", ToolInput: json.RawMessage(`{"path":"README.md"}`)}),
		ev(2, ports.SessionEvent{Type: "tool_result", Text: "# Demo"}),
		ev(3, ports.SessionEvent{Type: "usage", Text: "A demo project."}),
	}, nil
}

type recordingDefault bool

func (d recordingDefault) Effective(context.Context, string, string) (domain.EffectiveAgentSystem, error) {
	var e domain.EffectiveAgentSystem
	e.Score.TrajectoryRecording.Default = domain.AgentSystemField[bool]{Value: bool(d), Source: domain.SourceProject}
	return e, nil
}

func trajectoryHandler(t *testing.T, def bool) (*Handler, *bench.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := bench.New(bench.Deps{
		Cases:        sqlite.NewBenchCaseRepository(db),
		Trajectories: sqlite.NewBenchTrajectoryRepository(db),
		Tasks:        recordingTasks{"t1": {ID: "t1", ProjectID: "p1"}},
		SessionLog:   recordedSession{},
		Settings:     recordingDefault(def),
		Projects:     agentSystemProjects{"p1": {ID: "p1"}},
	})
	return NewHandler(Services{Bench: svc, BenchTrajectories: svc}), svc
}

func putJSON(t *testing.T, h *Handler, path, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, h, http.MethodPut, "/api"+path, body)
}

func TestATasksRecordingFlagIsSetReadAndFollowsTheDefaultWhenCleared(t *testing.T) {
	h, _ := trajectoryHandler(t, true)

	code, rec := doJSON(t, h, http.MethodGet, "/api/projects/p1/tasks/t1/trajectory-recording", "")
	if code != http.StatusOK || rec["enabled"] != true || rec["source"] != "project" || rec["explicit"] != nil {
		t.Fatalf("default = %d %v", code, rec)
	}
	code, rec = putJSON(t, h, "/projects/p1/tasks/t1/trajectory-recording", `{"enabled": false}`)
	if code != http.StatusOK || rec["enabled"] != false || rec["source"] != "task" || rec["explicit"] != false {
		t.Fatalf("off = %d %v", code, rec)
	}
	code, rec = putJSON(t, h, "/projects/p1/tasks/t1/trajectory-recording", `{"enabled": null}`)
	if code != http.StatusOK || rec["enabled"] != true || rec["explicit"] != nil {
		t.Fatalf("cleared = %d %v", code, rec)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/projects/p1/tasks/nope/trajectory-recording", ""); code != http.StatusNotFound {
		t.Fatalf("unknown task = %d, want 404", code)
	}
}

func TestARecordedTrajectoryIsListedAndMadeACase(t *testing.T) {
	h, svc := trajectoryHandler(t, true)
	if err := svc.RecordSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}

	code, list := doJSON(t, h, http.MethodGet, "/api/projects/p1/bench/trajectories", "")
	if code != http.StatusOK || len(list["trajectories"].([]any)) != 1 {
		t.Fatalf("list = %d %v", code, list)
	}
	code, c := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases/from-trajectory", `{"trajectory_id":"s1"}`)
	if code != http.StatusCreated || c["source"] != "trajectory" {
		t.Fatalf("case = %d %v", code, c)
	}
	tools := c["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["result"] != "# Demo" {
		t.Fatalf("tools = %v", tools)
	}
	if code, _ := doJSON(t, h, http.MethodPost, "/api/projects/p1/bench/cases/from-trajectory", `{"trajectory_id":"nope"}`); code != http.StatusNotFound {
		t.Fatalf("unknown trajectory = %d, want 404", code)
	}
	// The case is on the project like any other.
	_, cases := doJSON(t, h, http.MethodGet, "/api/projects/p1/bench/cases", "")
	if len(cases["cases"].([]any)) != 1 {
		t.Fatalf("cases = %v", cases)
	}
}

// The Edit form says "on, from this task; project default off": the answer
// carries the default the task would follow beside what holds.
func TestATasksRecordingAnswerNamesTheDefaultItFollowsWhenTheTaskSetsItsOwn(t *testing.T) {
	h, _ := trajectoryHandler(t, false)

	_, rec := doJSON(t, h, http.MethodGet, "/api/projects/p1/tasks/t1/trajectory-recording", "")
	if rec["default"] != false || rec["default_source"] != "project" {
		t.Fatalf("inheriting = %v", rec)
	}
	code, rec := putJSON(t, h, "/projects/p1/tasks/t1/trajectory-recording", `{"enabled": true}`)
	if code != http.StatusOK || rec["enabled"] != true || rec["source"] != "task" || rec["default"] != false || rec["default_source"] != "project" {
		t.Fatalf("on = %d %v", code, rec)
	}
}
