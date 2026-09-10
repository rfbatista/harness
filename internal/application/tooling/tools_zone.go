package tooling

import (
	"context"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

func ZoneTools(bpSvc *blueprint.Service) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_zones",
			Description: "Return all zones for the given project.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				if projectID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				zones := bpSvc.ListZones(projectID)
				return map[string]any{"zones": zones}, nil
			},
		},
		{
			Name:        "get_zone",
			Description: "Return one zone by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"}},"required":["zone_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				if zoneID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id is required"}
				}
				z := bpSvc.GetZone(zoneID)
				if z == nil {
					return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
				}
				return map[string]any{"zone": z}, nil
			},
		},
		{
			Name:        "create_zone",
			Description: "Create a zone in the given project with optional metadata and pattern.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"name":{"type":"string","description":"Zone name"},"pattern":{"type":"string","description":"Regex pattern"},"purpose":{"type":"string","description":"Purpose"},"constraints":{"type":"array","items":{"type":"string"},"description":"Constraints"},"assigned_agents":{"description":"Assigned agents (array of {id, name})"}},"required":["project_id","name"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				name := getString(args, "name", "")
				if projectID == "" || name == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id and name are required"}
				}
				pattern := getString(args, "pattern", "")
				purpose := getString(args, "purpose", "")
				rules := extractPromptRefs(args, "constraints")
				agents := extractAgentRefs(args, "assigned_agents")
				z, err := bpSvc.CreateZone(projectID, name, pattern, purpose, rules, agents)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
		{
			Name:        "update_zone",
			Description: "Update zone name, pattern, purpose, constraints, assigned_agents.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"},"name":{"type":"string","description":"Zone name"},"pattern":{"type":"string","description":"Regex pattern"},"purpose":{"type":"string","description":"Purpose"},"constraints":{"type":"array","items":{"type":"string"},"description":"Constraints"},"assigned_agents":{"description":"Assigned agents (array of {id, name})"}},"required":["zone_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				if zoneID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id is required"}
				}
				name := getString(args, "name", "")
				pattern := getString(args, "pattern", "")
				purpose := getString(args, "purpose", "")
				rules := extractPromptRefs(args, "constraints")
				agents := extractAgentRefs(args, "assigned_agents")
				z, err := bpSvc.UpdateZone(zoneID, name, pattern, purpose, rules, agents)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
		{
			Name:        "assign_path_to_zone",
			Description: "Add a path to a zone's explicit path set.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"},"path":{"type":"string","description":"Path to assign"}},"required":["zone_id","path"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				path := getString(args, "path", "")
				if zoneID == "" || path == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id and path are required"}
				}
				z, err := bpSvc.AssignPathToZone(zoneID, path)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
		{
			Name:        "remove_path_from_zone",
			Description: "Remove a path from a zone's explicit path set. No-op if the path was not explicitly assigned.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"},"path":{"type":"string","description":"Path to remove"}},"required":["zone_id","path"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				path := getString(args, "path", "")
				if zoneID == "" || path == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id and path are required"}
				}
				z, err := bpSvc.UnassignPathFromZone(zoneID, path)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
	}
}

func extractAgentRefs(args map[string]any, key string) []domain.Agent {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []domain.Agent
	for _, v := range slice {
		if m, ok := v.(map[string]any); ok {
			id, _ := m["id"].(string)
			n, _ := m["name"].(string)
			out = append(out, domain.Agent{ID: id, Name: n})
		}
	}
	return out
}

func extractPromptRefs(args map[string]any, key string) []domain.Prompt {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []domain.Prompt
	for _, v := range slice {
		if m, ok := v.(map[string]any); ok {
			id, _ := m["id"].(string)
			n, _ := m["name"].(string)
			out = append(out, domain.Prompt{ID: id, Name: n})
		}
	}
	return out
}
