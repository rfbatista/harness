package domain

import "time"

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// Task records an AI execution dispatched to a Zone within a Project.
// The Zone selects the Agent; all three IDs are stored for audit.
type Task struct {
	ID          string     `json:"id"`
	ZoneID      string     `json:"zone_id"`
	AgentID     string     `json:"agent_id"`
	ProjectID   string     `json:"project_id"`
	Instruction string     `json:"instruction"`
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
