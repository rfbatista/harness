package execution

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// RunTaskRequest is the input for dispatching a task to a zone.
type RunTaskRequest struct {
	ProjectID   string `json:"project_id"`
	ZoneID      string `json:"zone_id"`
	AgentID     string `json:"agent_id,omitempty"`
	Instruction string `json:"instruction"`
}

// Service orchestrates AI task execution within zone boundaries.
type Service struct {
	g        *genkit.Genkit
	resolver *ContextResolver
	tasks    ports.TaskRepository
	fsTools  []domain.Tool
}

// NewService creates an execution service. fsTools are the raw filesystem
// tools that will be wrapped with zone-boundary validation at runtime.
func NewService(g *genkit.Genkit, resolver *ContextResolver, tasks ports.TaskRepository, fsTools []domain.Tool) *Service {
	return &Service{
		g:        g,
		resolver: resolver,
		tasks:    tasks,
		fsTools:  fsTools,
	}
}

// RunTask validates the zone/project, resolves the agent, builds scoped tools,
// and runs Genkit generation. The task lifecycle is persisted.
func (s *Service) RunTask(ctx context.Context, req RunTaskRequest) (*domain.Task, error) {
	log := slog.With("project", req.ProjectID, "zone", req.ZoneID)
	log.Info("RunTask started", "agent", req.AgentID, "instruction_len", len(req.Instruction))

	if req.ProjectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if req.ZoneID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id is required"}
	}
	if req.Instruction == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "instruction is required"}
	}

	ec, err := s.resolver.Resolve(req.ProjectID, req.ZoneID, req.AgentID)
	if err != nil {
		log.Error("context resolution failed", "error", err)
		return nil, err
	}
	log.Info("context resolved",
		"agent", ec.Agent.ID,
		"root", ec.AllowedRoot,
		"zone_pattern", ec.ZonePattern,
		"explicit_paths", ec.ExplicitPaths,
		"system_prompt_len", len(ec.SystemPrompt),
	)

	task, err := s.tasks.Create(&domain.Task{
		ZoneID:      req.ZoneID,
		AgentID:     ec.Agent.ID,
		ProjectID:   req.ProjectID,
		Instruction: req.Instruction,
		Status:      domain.TaskStatusPending,
	})
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	log = log.With("task_id", task.ID)
	log.Info("task created, starting generation")

	_ = s.tasks.UpdateStatus(task.ID, domain.TaskStatusRunning, "", "")

	tools := ScopedFilesystemTools(s.g, ec, s.fsTools)

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = fmt.Sprintf("%v", t)
	}
	log.Info("scoped tools built", "tools_count", len(tools), "tools", toolNames)

	maxTurns := 10
	if v := os.Getenv("GENKIT_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTurns = n
		}
	}
	log.Info("calling genkit.Generate", "max_turns", maxTurns)

	resp, err := genkit.Generate(ctx, s.g,
		ai.WithSystem(ec.SystemPrompt),
		ai.WithPrompt(req.Instruction),
		ai.WithTools(tools...),
		ai.WithMaxTurns(maxTurns),
	)
	if err != nil {
		log.Error("generation failed", "error", err)
		_ = s.tasks.UpdateStatus(task.ID, domain.TaskStatusFailed, "", err.Error())
		task.Status = domain.TaskStatusFailed
		task.Error = err.Error()
		return task, nil
	}

	result := resp.Text()
	log.Info("generation completed", "result_len", len(result), "usage", resp.Usage)
	_ = s.tasks.UpdateStatus(task.ID, domain.TaskStatusCompleted, result, "")
	task.Status = domain.TaskStatusCompleted
	task.Result = result
	return task, nil
}

// GetTask returns a task by ID.
func (s *Service) GetTask(id string) *domain.Task {
	return s.tasks.Get(id)
}

// ListTasks returns tasks matching the given filter.
func (s *Service) ListTasks(filter ports.TaskFilter) []*domain.Task {
	return s.tasks.List(filter)
}
