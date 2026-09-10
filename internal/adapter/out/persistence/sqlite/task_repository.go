package sqlite

import (
	"time"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"gorm.io/gorm"
)

var _ ports.TaskRepository = (*TaskRepository)(nil)

// TaskRepository persists tasks in SQLite via GORM.
type TaskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

func (r *TaskRepository) Get(id string) *domain.Task {
	var m TaskModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *TaskRepository) List(filter ports.TaskFilter) []*domain.Task {
	q := r.db.Model(&TaskModel{})
	if filter.ZoneID != "" {
		q = q.Where("zone_id = ?", filter.ZoneID)
	}
	if filter.AgentID != "" {
		q = q.Where("agent_id = ?", filter.AgentID)
	}
	if filter.ProjectID != "" {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", string(filter.Status))
	}
	q = q.Order("created_at DESC")

	var models []TaskModel
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Task, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *TaskRepository) Create(task *domain.Task) (*domain.Task, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	m := &TaskModel{
		ID:          id,
		ZoneID:      task.ZoneID,
		AgentID:     task.AgentID,
		ProjectID:   task.ProjectID,
		Instruction: task.Instruction,
		Status:      string(task.Status),
		Result:      task.Result,
		Error:       task.Error,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TaskRepository) UpdateStatus(id string, status domain.TaskStatus, result, errMsg string) error {
	updates := map[string]any{
		"status":     string(status),
		"result":     result,
		"error":      errMsg,
		"updated_at": time.Now().UnixMilli(),
	}
	res := r.db.Model(&TaskModel{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "task not found"}
	}
	return nil
}
