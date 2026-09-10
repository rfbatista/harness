package tooling

import (
	"context"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

func BoundedContextTools(bpSvc *blueprint.Service) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_bounded_contexts",
			Description: "Return all bounded contexts (DDD) for the given project.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				if projectID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				return map[string]any{"bounded_contexts": bpSvc.ListBoundedContexts(projectID)}, nil
			},
		},
		{
			Name:        "get_bounded_context",
			Description: "Return one bounded context by id, including its purpose and ubiquitous language.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"bounded_context_id":{"type":"string","description":"Bounded context ID"}},"required":["bounded_context_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "bounded_context_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "bounded_context_id is required"}
				}
				bc := bpSvc.GetBoundedContext(id)
				if bc == nil {
					return nil, &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
				}
				return map[string]any{"bounded_context": bc}, nil
			},
		},
		{
			Name:        "create_bounded_context",
			Description: "Create a bounded context (DDD) in the given project with a purpose and optional ubiquitous language terms.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"name":{"type":"string","description":"Bounded context name"},"purpose":{"type":"string","description":"What this context is responsible for"},"ubiquitous_language":{"type":"array","items":{"type":"object","properties":{"term":{"type":"string"},"definition":{"type":"string"}}},"description":"Domain terms and their meaning inside this context"}},"required":["project_id","name"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				name := getString(args, "name", "")
				if projectID == "" || name == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id and name are required"}
				}
				purpose := getString(args, "purpose", "")
				terms := extractLanguageTerms(args, "ubiquitous_language")
				bc, err := bpSvc.CreateBoundedContext(projectID, name, purpose, terms)
				if err != nil {
					return nil, err
				}
				return map[string]any{"bounded_context": bc}, nil
			},
		},
		{
			Name:        "update_bounded_context",
			Description: "Update a bounded context's name, purpose, and ubiquitous language (terms are replaced wholesale).",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"bounded_context_id":{"type":"string","description":"Bounded context ID"},"name":{"type":"string","description":"Bounded context name"},"purpose":{"type":"string","description":"What this context is responsible for"},"ubiquitous_language":{"type":"array","items":{"type":"object","properties":{"term":{"type":"string"},"definition":{"type":"string"}}},"description":"Domain terms and their meaning inside this context"}},"required":["bounded_context_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "bounded_context_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "bounded_context_id is required"}
				}
				name := getString(args, "name", "")
				purpose := getString(args, "purpose", "")
				terms := extractLanguageTerms(args, "ubiquitous_language")
				bc, err := bpSvc.UpdateBoundedContext(id, name, purpose, terms)
				if err != nil {
					return nil, err
				}
				return map[string]any{"bounded_context": bc}, nil
			},
		},
		{
			Name:        "delete_bounded_context",
			Description: "Delete a bounded context. Zones linked to it are unlinked, not deleted.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"bounded_context_id":{"type":"string","description":"Bounded context ID"}},"required":["bounded_context_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "bounded_context_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "bounded_context_id is required"}
				}
				if err := bpSvc.DeleteBoundedContext(id); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": true}, nil
			},
		},
		{
			Name:        "assign_zone_to_bounded_context",
			Description: "Link a zone to a bounded context of the same project. A zone belongs to at most one context.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"},"bounded_context_id":{"type":"string","description":"Bounded context ID"}},"required":["zone_id","bounded_context_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				bcID := getString(args, "bounded_context_id", "")
				if zoneID == "" || bcID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id and bounded_context_id are required"}
				}
				z, err := bpSvc.AssignZoneToBoundedContext(zoneID, bcID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
		{
			Name:        "unassign_zone_from_bounded_context",
			Description: "Clear a zone's bounded context link.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"zone_id":{"type":"string","description":"Zone ID"}},"required":["zone_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				zoneID := getString(args, "zone_id", "")
				if zoneID == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "zone_id is required"}
				}
				z, err := bpSvc.UnassignZoneFromBoundedContext(zoneID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"zone": z}, nil
			},
		},
	}
}

func extractLanguageTerms(args map[string]any, key string) []domain.LanguageTerm {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []domain.LanguageTerm
	for _, v := range slice {
		if m, ok := v.(map[string]any); ok {
			term, _ := m["term"].(string)
			def, _ := m["definition"].(string)
			out = append(out, domain.LanguageTerm{Term: term, Definition: def})
		}
	}
	return out
}
