package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// TaskRunner is the driving port of the execution service: one-shot AI tasks
// dispatched to a zone. *execution.Service satisfies it.
type TaskRunner interface {
	RunTask(ctx context.Context, req RunTaskRequest) (*domain.Task, error)
	GetTask(id string) *domain.Task
	ListTasks(filter TaskFilter) []*domain.Task
}

// RunTaskRequest dispatches an instruction to a zone of a project.
type RunTaskRequest struct {
	ProjectID   string `json:"project_id"`
	ZoneID      string `json:"zone_id"`
	AgentID     string `json:"agent_id,omitempty"`
	Instruction string `json:"instruction"`
}
