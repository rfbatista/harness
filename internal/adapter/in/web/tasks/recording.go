package tasks

import (
	"context"
	"log/slog"

	"operators-mcp/internal/domain"
)

// RecordingReader says whether a task's sessions are recorded as bench
// trajectories (ADR 0015): the task's own flag and the settings default it
// follows. The eval bench's service is one.
type RecordingReader interface {
	TaskRecording(ctx context.Context, projectID, taskID, repositoryID string) (domain.TaskTrajectoryRecording, error)
}

// recording seeds the Edit form's Trajectory recording control; nil when
// recordings are not served or could not be read (the form then goes without
// the control).
func (h Handler) recording(ctx context.Context, projectID, taskID string) *domain.TaskTrajectoryRecording {
	if h.Recordings == nil {
		return nil
	}
	rec, err := h.Recordings.TaskRecording(ctx, projectID, taskID, "")
	if err != nil {
		slog.Warn("task page: trajectory recording not read", "task", taskID, "err", err)
		return nil
	}
	return &rec
}
