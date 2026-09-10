package tooling

import (
	"context"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

func TreeTools(bpSvc *blueprint.Service) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_tree",
			Description: "Return the project's folder structure as a hierarchical tree. Use project_id or root to specify the base directory.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"root":{"type":"string","description":"Root path (optional)"},"project_id":{"type":"string","description":"Project ID (optional)"},"depth":{"type":"number","description":"Max depth (optional)"}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				root := getString(args, "root", "")
				projectID := getString(args, "project_id", "")
				tree, err := bpSvc.ListTree(root, projectID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"tree": tree}, nil
			},
		},
		{
			Name:        "list_matching_paths",
			Description: "Return paths under project root that match the given regex pattern. Use project_id or root to specify the base directory.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern"},"root":{"type":"string","description":"Root path (optional)"},"project_id":{"type":"string","description":"Project ID (optional)"}},"required":["pattern"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				pattern := getString(args, "pattern", "")
				if pattern == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "pattern is required"}
				}
				root := getString(args, "root", "")
				projectID := getString(args, "project_id", "")
				paths, err := bpSvc.ListMatchingPaths(root, projectID, pattern)
				if err != nil {
					return nil, err
				}
				return map[string]any{"paths": paths}, nil
			},
		},
	}
}
