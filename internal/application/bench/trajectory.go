package bench

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchTrajectories = (*Service)(nil)

// taskOf finds the task in the project.
func (s *Service) taskOf(ctx context.Context, projectID, taskID string) error {
	if err := s.checkProject(ctx, projectID); err != nil {
		return err
	}
	if s.d.Tasks == nil {
		return &domain.StructuredError{Code: "BENCH_UNAVAILABLE", Message: "trajectory recording is not configured"}
	}
	if t := s.d.Tasks.Get(ctx, taskID); t == nil || t.ProjectID != projectID {
		return &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "task not found: " + taskID}
	}
	return nil
}

// TaskRecording says whether the task's sessions are recorded.
func (s *Service) TaskRecording(ctx context.Context, projectID, taskID, repositoryID string) (domain.TaskTrajectoryRecording, error) {
	out := domain.TaskTrajectoryRecording{TaskID: taskID}
	if err := s.taskOf(ctx, projectID, taskID); err != nil {
		return out, err
	}
	explicit, err := s.d.Trajectories.TaskFlag(ctx, taskID)
	if err != nil {
		return out, err
	}
	if s.d.Settings != nil {
		eff, err := s.d.Settings.Effective(ctx, projectID, repositoryID)
		switch {
		case err == nil:
			def := eff.Score.TrajectoryRecording.Default
			out.Default, out.DefaultSource = def.Value, string(def.Source)
		case explicit == nil:
			return out, err
		}
		// The task's own flag decides without the default: a settings read
		// that failed only leaves the resolution line without it.
	}
	if explicit != nil {
		out.Enabled, out.Explicit, out.Source = *explicit, explicit, "task"
		return out, nil
	}
	out.Enabled, out.Source = out.Default, out.DefaultSource
	return out, nil
}

// SetTaskRecording opts the task in or out of recording; nil makes it follow
// the settings default again.
func (s *Service) SetTaskRecording(ctx context.Context, projectID, taskID string, enabled *bool) (domain.TaskTrajectoryRecording, error) {
	if err := s.taskOf(ctx, projectID, taskID); err != nil {
		return domain.TaskTrajectoryRecording{TaskID: taskID}, err
	}
	if err := s.d.Trajectories.SetTaskFlag(ctx, projectID, taskID, enabled); err != nil {
		return domain.TaskTrajectoryRecording{TaskID: taskID}, err
	}
	return s.TaskRecording(ctx, projectID, taskID, "")
}

// RecordSession leaves the session's trajectory when the session ended on a
// task that is recorded, built from the session's stored events: its tool
// calls with their parameters and results, and its final state. A session of
// a task not recorded, or of no task, leaves nothing.
func (s *Service) RecordSession(ctx context.Context, sessionID string) error {
	if s.d.Trajectories == nil || s.d.SessionLog == nil || s.d.Tasks == nil {
		return nil
	}
	sess, err := s.d.SessionLog.Find(ctx, sessionID)
	if err != nil || sess == nil || sess.TicketID == "" {
		return err
	}
	rec, err := s.TaskRecording(ctx, sess.ProjectID, sess.TicketID, sess.RepositoryID)
	if err != nil || !rec.Enabled {
		return err
	}
	events, err := s.d.SessionLog.ListEvents(sessionID, 0)
	if err != nil {
		return err
	}
	t := trajectoryOf(sess, events)
	t.RecordedAt = time.Now().UTC().Truncate(time.Second)
	return s.d.Trajectories.SaveTrajectory(ctx, t)
}

// trajectoryOf folds a session's events into its trajectory. A result answers
// the oldest call still waiting for one.
func trajectoryOf(sess *domain.Session, events []ports.StoredEvent) domain.BenchTrajectory {
	t := domain.BenchTrajectory{ID: sess.ID, ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sess.ID, Steps: []domain.BenchTrajectoryStep{}}
	var waiting []int
	var lastOutput string
	for _, stored := range events {
		var ev ports.SessionEvent
		if json.Unmarshal(stored.Payload, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "user_message":
			if t.Prompt == "" {
				t.Prompt = ev.Text
			}
		case "tool_use":
			step := domain.BenchTrajectoryStep{Tool: ev.ToolName}
			_ = json.Unmarshal(ev.ToolInput, &step.Params)
			t.Steps = append(t.Steps, step)
			waiting = append(waiting, len(t.Steps)-1)
		case "tool_result":
			if len(waiting) > 0 {
				t.Steps[waiting[0]].Result = ev.Text
				t.Steps[waiting[0]].IsError = ev.IsError
				waiting = waiting[1:]
			}
		case "output":
			if ev.Text != "" {
				lastOutput = ev.Text
			}
		case "usage":
			if ev.Text != "" {
				t.FinalState = ev.Text
			}
		}
	}
	if t.FinalState == "" {
		t.FinalState = lastOutput
	}
	if t.Prompt == "" {
		t.Prompt = sess.Task
	}
	return t
}

// ListTrajectories answers the project's recorded trajectories, newest first.
func (s *Service) ListTrajectories(ctx context.Context, projectID string) ([]domain.BenchTrajectory, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return nil, err
	}
	if s.d.Trajectories == nil {
		return nil, nil
	}
	return s.d.Trajectories.ListTrajectories(ctx, projectID)
}

// GetTrajectory answers one recorded trajectory of the project.
func (s *Service) GetTrajectory(ctx context.Context, projectID, trajectoryID string) (domain.BenchTrajectory, error) {
	if err := s.checkProject(ctx, projectID); err != nil {
		return domain.BenchTrajectory{}, err
	}
	if s.d.Trajectories != nil {
		t, err := s.d.Trajectories.GetTrajectory(ctx, projectID, trajectoryID)
		if err != nil {
			return domain.BenchTrajectory{}, err
		}
		if t != nil {
			return *t, nil
		}
	}
	return domain.BenchTrajectory{}, &domain.StructuredError{Code: "BENCH_TRAJECTORY_NOT_FOUND", Message: "trajectory not found: " + trajectoryID}
}

// CreateCaseFromTrajectory stores a case that replays a recording: its prompt;
// a mock for each tool it called, answering with the result the recording
// saw; the calls with their parameters, in order, as what to expect; and the
// final state as the reference result.
func (s *Service) CreateCaseFromTrajectory(ctx context.Context, projectID, trajectoryID string) (domain.BenchCase, error) {
	t, err := s.GetTrajectory(ctx, projectID, trajectoryID)
	if err != nil {
		return domain.BenchCase{}, err
	}
	def := domain.BenchCase{Name: "Recorded: " + firstLine(t.Prompt, 80), Prompt: t.Prompt, Source: domain.BenchSourceTrajectory, Tools: []domain.BenchTool{}}
	def.Expect.Ordered, def.Expect.Reference = true, t.FinalState
	mocked := map[string]bool{}
	for _, step := range t.Steps {
		if !mocked[step.Tool] {
			mocked[step.Tool] = true
			def.Tools = append(def.Tools, domain.BenchTool{Name: step.Tool, Result: step.Result, IsError: step.IsError})
		}
		def.Expect.Tools = append(def.Expect.Tools, domain.BenchExpectedCall{Tool: step.Tool, Params: step.Params})
	}
	return s.CreateCase(ctx, projectID, def)
}

// firstLine is the start of s up to its first line break, at most n runes.
func firstLine(s string, n int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
