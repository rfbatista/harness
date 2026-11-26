package bench

import (
	"context"
	"encoding/json"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type memTrajectories struct {
	saved map[string]domain.BenchTrajectory
	flags map[string]bool
}

func newMemTrajectories() *memTrajectories {
	return &memTrajectories{saved: map[string]domain.BenchTrajectory{}, flags: map[string]bool{}}
}

func (m *memTrajectories) DeleteProject(context.Context, string) error { return nil }

func (m *memTrajectories) SaveTrajectory(_ context.Context, t domain.BenchTrajectory) error {
	m.saved[t.ID] = t
	return nil
}
func (m *memTrajectories) GetTrajectory(_ context.Context, projectID, id string) (*domain.BenchTrajectory, error) {
	if t, ok := m.saved[id]; ok && t.ProjectID == projectID {
		return &t, nil
	}
	return nil, nil
}
func (m *memTrajectories) ListTrajectories(_ context.Context, projectID string) ([]domain.BenchTrajectory, error) {
	var out []domain.BenchTrajectory
	for _, t := range m.saved {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *memTrajectories) TaskFlag(_ context.Context, taskID string) (*bool, error) {
	if v, ok := m.flags[taskID]; ok {
		return &v, nil
	}
	return nil, nil
}
func (m *memTrajectories) SetTaskFlag(_ context.Context, _, taskID string, enabled *bool) error {
	if enabled == nil {
		delete(m.flags, taskID)
	} else {
		m.flags[taskID] = *enabled
	}
	return nil
}

type memTasks map[string]*domain.Ticket

func (m memTasks) Get(_ context.Context, id string) *domain.Ticket { return m[id] }

// sessionLog is one session with its stored events.
type sessionLog struct {
	session *domain.Session
	events  []ports.StoredEvent
}

func (l sessionLog) Find(context.Context, string) (*domain.Session, error) { return l.session, nil }
func (l sessionLog) ListEvents(string, int64) ([]ports.StoredEvent, error) { return l.events, nil }

func stored(seq int64, ev ports.SessionEvent) ports.StoredEvent {
	b, _ := json.Marshal(ev)
	return ports.StoredEvent{Seq: seq, Type: ev.Type, Payload: b}
}

// defaults answers the recording default per repository: the project's, and an
// override for one repository.
type defaults struct {
	project  bool
	override map[string]bool
}

func (d defaults) Effective(_ context.Context, _, repositoryID string) (domain.EffectiveAgentSystem, error) {
	var e domain.EffectiveAgentSystem
	e.Score.TrajectoryRecording.Default = domain.AgentSystemField[bool]{Value: d.project, Source: domain.SourceProject}
	if v, ok := d.override[repositoryID]; ok && repositoryID != "" {
		e.Score.TrajectoryRecording.Default = domain.AgentSystemField[bool]{Value: v, Source: domain.SourceRepository}
	}
	return e, nil
}

func trajectoryService(def defaults, sess sessionLog) (*Service, *memTrajectories, *memCases) {
	tr, cases := newMemTrajectories(), &memCases{}
	return New(Deps{
		Cases:        cases,
		Trajectories: tr,
		Tasks:        memTasks{"t1": {ID: "t1", ProjectID: "p1"}, "t2": {ID: "t2", ProjectID: "other"}},
		SessionLog:   sess,
		Settings:     def,
		Projects:     projects{"p1": true},
	}), tr, cases
}

type projects map[string]bool

func (p projects) Get(_ context.Context, id string) *domain.Project {
	if p[id] {
		return &domain.Project{ID: id}
	}
	return nil
}

type memCases struct{ cases []domain.BenchCase }

func (m *memCases) List(_ context.Context, projectID string) ([]domain.BenchCase, error) {
	return m.cases, nil
}
func (m *memCases) Save(_ context.Context, c domain.BenchCase) error {
	m.cases = append(m.cases, c)
	return nil
}
func (m *memCases) Delete(context.Context, string, string) (bool, error) { return false, nil }
func (m *memCases) DeleteProject(context.Context, string) error          { return nil }

var toolSession = sessionLog{
	session: &domain.Session{ID: "s1", ProjectID: "p1", RepositoryID: "r1", TicketID: "t1", Task: "Read the README"},
	events: []ports.StoredEvent{
		stored(1, ports.SessionEvent{Type: "user_message", Text: "Read the README and say what it is."}),
		stored(2, ports.SessionEvent{Type: "tool_use", ToolName: "read_file", ToolInput: json.RawMessage(`{"path":"README.md"}`)}),
		stored(3, ports.SessionEvent{Type: "tool_result", Text: "# Demo"}),
		stored(4, ports.SessionEvent{Type: "tool_use", ToolName: "edit_file", ToolInput: json.RawMessage(`{"path":"main.go"}`)}),
		stored(5, ports.SessionEvent{Type: "tool_result", Text: "edited"}),
		stored(6, ports.SessionEvent{Type: "output", Text: "thinking about it"}),
		stored(7, ports.SessionEvent{Type: "usage", Text: "It is a demo project."}),
	},
}

func TestATaskFollowsTheProjectDefaultAndARepositoryOverrideUntilItSetsItsOwn(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := trajectoryService(defaults{project: true, override: map[string]bool{"r1": false}}, toolSession)

	got, err := svc.TaskRecording(ctx, "p1", "t1", "")
	if err != nil || !got.Enabled || got.Source != string(domain.SourceProject) || got.Explicit != nil {
		t.Fatalf("project default = %+v, %v", got, err)
	}
	got, _ = svc.TaskRecording(ctx, "p1", "t1", "r1")
	if got.Enabled || got.Source != string(domain.SourceRepository) {
		t.Fatalf("repository override = %+v", got)
	}

	on := true
	got, err = svc.SetTaskRecording(ctx, "p1", "t1", &on)
	if err != nil || !got.Enabled || got.Source != "task" || got.Explicit == nil || !*got.Explicit {
		t.Fatalf("explicit on = %+v, %v", got, err)
	}
	if got, _ := svc.TaskRecording(ctx, "p1", "t1", "r1"); !got.Enabled {
		t.Fatalf("the task's own choice lost to the repository default: %+v", got)
	}
	got, _ = svc.SetTaskRecording(ctx, "p1", "t1", nil)
	if got.Explicit != nil || got.Source != string(domain.SourceProject) {
		t.Fatalf("cleared = %+v", got)
	}
}

func TestATaskOfAnotherProjectOrNoTaskIsNotFound(t *testing.T) {
	svc, _, _ := trajectoryService(defaults{}, toolSession)
	for _, task := range []string{"t2", "nope"} {
		if _, err := svc.TaskRecording(context.Background(), "p1", task, ""); err == nil {
			t.Fatalf("task %s was found in p1", task)
		}
	}
}

func TestASessionOnATaskWithRecordingOnLeavesItsTrajectoryInTheCaseFormat(t *testing.T) {
	svc, store, _ := trajectoryService(defaults{project: true}, toolSession)
	if err := svc.RecordSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	got, ok := store.saved["s1"]
	if !ok {
		t.Fatal("nothing was recorded")
	}
	if got.ProjectID != "p1" || got.TaskID != "t1" || got.SessionID != "s1" || got.Prompt != "Read the README and say what it is." {
		t.Fatalf("trajectory = %+v", got)
	}
	if len(got.Steps) != 2 || got.Steps[0].Tool != "read_file" || got.Steps[0].Params["path"] != "README.md" || got.Steps[0].Result != "# Demo" || got.Steps[1].Result != "edited" {
		t.Fatalf("steps = %+v", got.Steps)
	}
	if got.FinalState != "It is a demo project." {
		t.Fatalf("final state = %q", got.FinalState)
	}
}

func TestASessionOnATaskWithRecordingOffRecordsNothing(t *testing.T) {
	svc, store, _ := trajectoryService(defaults{project: false}, toolSession)
	if err := svc.RecordSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("recorded %+v", store.saved)
	}
	// A session with no task is not recorded either, whatever the default.
	noTask := toolSession
	noTask.session = &domain.Session{ID: "s2", ProjectID: "p1"}
	svc, store, _ = trajectoryService(defaults{project: true}, noTask)
	_ = svc.RecordSession(context.Background(), "s2")
	if len(store.saved) != 0 {
		t.Fatalf("recorded %+v", store.saved)
	}
}

func TestARecordedTrajectoryBecomesACaseWithItsCallsInOrderAndTheirCannedResults(t *testing.T) {
	ctx := context.Background()
	svc, _, cases := trajectoryService(defaults{project: true}, toolSession)
	_ = svc.RecordSession(ctx, "s1")

	c, err := svc.CreateCaseFromTrajectory(ctx, "p1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != domain.BenchSourceTrajectory || c.ProjectID != "p1" || c.Prompt != "Read the README and say what it is." || len(cases.cases) != 1 {
		t.Fatalf("case = %+v", c)
	}
	if len(c.Tools) != 2 || c.Tools[0].Name != "read_file" || c.Tools[0].Result != "# Demo" || c.Tools[1].Result != "edited" {
		t.Fatalf("tools = %+v", c.Tools)
	}
	if len(c.Expect.Tools) != 2 || c.Expect.Tools[1].Tool != "edit_file" || !c.Expect.Ordered || c.Expect.Reference != "It is a demo project." {
		t.Fatalf("expect = %+v", c.Expect)
	}

	if _, err := svc.CreateCaseFromTrajectory(ctx, "p1", "nope"); err == nil {
		t.Fatal("an unknown trajectory made a case")
	}
}
