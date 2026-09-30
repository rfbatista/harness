package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func AgentTools(bpSvc ports.AgentCatalog) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_agents",
			Description: "Return all agents. Agents can be assigned to zones.",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				agents, err := bpSvc.ListAgents(ctx)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agents": agents}, nil
			},
		},
		{
			Name:        "get_agent",
			Description: "Return one agent by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"}},"required":["agent_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "agent_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "agent_id is required"}
				}
				a, err := bpSvc.GetAgent(ctx, id)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agent": a}, nil
			},
		},
		{
			Name:        "create_agent",
			Description: "Create an agent with optional name, description, prompt_id, and skill_ids.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"name":{"type":"string","description":"Agent name"},"description":{"type":"string","description":"Agent description"},"prompt_id":{"type":"string","description":"Prompt ID to associate with the agent"},"skill_ids":{"type":"array","items":{"type":"string"},"description":"Skill IDs to associate with the agent"}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				name := getString(args, "name", "")
				description := getString(args, "description", "")
				promptID := getString(args, "prompt_id", "")
				skillIDs := getStringSliceDefault(args, "skill_ids", []string{})
				mcpServerIDs := getStringSliceDefault(args, "mcp_server_ids", []string{})
				a, err := bpSvc.CreateAgent(ctx, name, description, promptID, skillIDs, mcpServerIDs)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agent": a}, nil
			},
		},
		{
			Name:        "update_agent",
			Description: "Update an agent's name, description, prompt_id, and/or skill_ids.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"},"name":{"type":"string","description":"Agent name"},"description":{"type":"string","description":"Agent description"},"prompt_id":{"type":"string","description":"Prompt ID to associate with the agent"},"skill_ids":{"type":"array","items":{"type":"string"},"description":"Skill IDs to associate with the agent"}},"required":["agent_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "agent_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "agent_id is required"}
				}
				name := getString(args, "name", "")
				description := getString(args, "description", "")
				promptID := getString(args, "prompt_id", "")
				skillIDs := getStringSliceDefault(args, "skill_ids", []string{})
				mcpServerIDs := getStringSliceDefault(args, "mcp_server_ids", []string{})
				a, err := bpSvc.UpdateAgent(ctx, id, name, description, promptID, skillIDs, mcpServerIDs)
				if err != nil {
					return nil, err
				}
				return map[string]any{"agent": a}, nil
			},
		},
		{
			Name:        "delete_agent",
			Description: "Delete an agent by id. The agent is removed from all zones that reference it.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"}},"required":["agent_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "agent_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "agent_id is required"}
				}
				if err := bpSvc.DeleteAgent(ctx, id); err != nil {
					return nil, err
				}
				return map[string]string{"deleted": id}, nil
			},
		},
	}
}
