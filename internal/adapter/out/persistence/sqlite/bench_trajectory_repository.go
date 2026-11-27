package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchTrajectoryStore = (*BenchTrajectoryRepository)(nil)

// BenchTrajectoryModel is the trajectory a session of a recorded task left
// (ADR 0015), whole as JSON; its id is the session's, so a session that ends
// again records over itself.
type BenchTrajectoryModel struct {
	ID         string `gorm:"primaryKey"`
	ProjectID  string `gorm:"column:project_id;index"`
	RecordedAt time.Time
	Data       string
}

// TableName overrides the table name.
func (BenchTrajectoryModel) TableName() string { return "bench_trajectories" }

// TaskTrajectoryFlagModel is a task's own choice to be recorded or not; a task
// with no row follows the settings default.
type TaskTrajectoryFlagModel struct {
	TaskID    string `gorm:"column:task_id;primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Enabled   bool
}

// TableName overrides the table name.
func (TaskTrajectoryFlagModel) TableName() string { return "task_trajectory_flags" }

// BenchTrajectoryRepository stores trajectories and the tasks' choices.
type BenchTrajectoryRepository struct{ db *gorm.DB }

// NewBenchTrajectoryRepository returns the store.
func NewBenchTrajectoryRepository(db *gorm.DB) *BenchTrajectoryRepository {
	return &BenchTrajectoryRepository{db: db}
}

// SaveTrajectory stores a trajectory, replacing the session's earlier one.
func (r *BenchTrajectoryRepository) SaveTrajectory(ctx context.Context, t domain.BenchTrajectory) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Save(&BenchTrajectoryModel{ID: t.ID, ProjectID: t.ProjectID, RecordedAt: t.RecordedAt, Data: string(data)}).Error
}

// GetTrajectory answers the project's trajectory, nil when there is none.
func (r *BenchTrajectoryRepository) GetTrajectory(ctx context.Context, projectID, id string) (*domain.BenchTrajectory, error) {
	var rows []BenchTrajectoryModel
	if err := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, id).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	var t domain.BenchTrajectory
	return &t, json.Unmarshal([]byte(rows[0].Data), &t)
}

// ListTrajectories answers the project's trajectories, newest first.
func (r *BenchTrajectoryRepository) ListTrajectories(ctx context.Context, projectID string) ([]domain.BenchTrajectory, error) {
	var rows []BenchTrajectoryModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("recorded_at desc, id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BenchTrajectory, 0, len(rows))
	for _, row := range rows {
		var t domain.BenchTrajectory
		if err := json.Unmarshal([]byte(row.Data), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// TaskFlag is the task's own choice, nil when it made none.
func (r *BenchTrajectoryRepository) TaskFlag(ctx context.Context, taskID string) (*bool, error) {
	var rows []TaskTrajectoryFlagModel
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0].Enabled, nil
}

// SetTaskFlag stores the task's choice, or clears it with nil. The project is
// a key for the cleanup when a project is deleted.
func (r *BenchTrajectoryRepository) SetTaskFlag(ctx context.Context, projectID, taskID string, enabled *bool) error {
	if enabled == nil {
		return r.db.WithContext(ctx).Where("task_id = ?", taskID).Delete(&TaskTrajectoryFlagModel{}).Error
	}
	return r.db.WithContext(ctx).Save(&TaskTrajectoryFlagModel{TaskID: taskID, ProjectID: projectID, Enabled: *enabled}).Error
}

// DeleteProject removes the project's trajectories and its tasks' choices.
func (r *BenchTrajectoryRepository) DeleteProject(ctx context.Context, projectID string) error {
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&TaskTrajectoryFlagModel{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&BenchTrajectoryModel{}).Error
}
