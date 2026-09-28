package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// TaskTools returns MCP tools for dispatching and querying zone tasks.
func TaskTools(execSvc ports.TaskRunner) []domain.Tool {
	return []domain.Tool{
		{
			Name: "dispatch_zone_task",
			Description: `Dispatch an AI task to a zone within a project.
The zone selects the agent (or specify agent_id explicitly).
The agent executes autonomously within the zone's file boundaries.
Returns the task with its result or error.`,
			InputSchema: schemaFromJSON(`{
				"type": "object",
				"properties": {
					"project_id": {
						"type": "string",
						"description": "Project ID that owns the zone"
					},
					"zone_id": {
						"type": "string",
						"description": "Zone ID to dispatch the task to"
					},
					"agent_id": {
						"type": "string",
						"description": "Optional: specific agent ID (zone auto-selects if omitted)"
					},
					"instruction": {
						"type": "string",
						"description": "What the agent should do"
					}
				},
				"required": ["project_id", "zone_id", "instruction"]
			}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				req := ports.RunTaskRequest{
					ProjectID:   getString(args, "project_id", ""),
					ZoneID:      getString(args, "zone_id", ""),
					AgentID:     getString(args, "agent_id", ""),
					Instruction: getString(args, "instruction", ""),
				}
				task, err := execSvc.RunTask(ctx, req)
				if err != nil {
					return nil, err
				}
				return map[string]any{"task": task}, nil
			},
		},
		{
			Name:        "list_zone_tasks",
			Description: "List tasks with optional filters (project_id, zone_id, status).",
			InputSchema: schemaFromJSON(`{
				"type": "object",
				"properties": {
					"project_id": {
						"type": "string",
						"description": "Filter by project ID"
					},
					"zone_id": {
						"type": "string",
						"description": "Filter by zone ID"
					},
					"status": {
						"type": "string",
						"description": "Filter by status (pending, running, completed, failed)"
					}
				}
			}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				filter := ports.TaskFilter{
					ProjectID: getString(args, "project_id", ""),
					ZoneID:    getString(args, "zone_id", ""),
					Status:    domain.TaskStatus(getString(args, "status", "")),
				}
				tasks := execSvc.ListTasks(filter)
				return map[string]any{"tasks": tasks}, nil
			},
		},
		{
			Name:        "get_zone_task",
			Description: "Get a task by ID.",
			InputSchema: schemaFromJSON(`{
				"type": "object",
				"properties": {
					"task_id": {
						"type": "string",
						"description": "Task ID"
					}
				},
				"required": ["task_id"]
			}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "task_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "task_id is required"}
				}
				task := execSvc.GetTask(id)
				if task == nil {
					return nil, &domain.StructuredError{Code: "TASK_NOT_FOUND", Message: "task not found"}
				}
				return map[string]any{"task": task}, nil
			},
		},
	}
}
