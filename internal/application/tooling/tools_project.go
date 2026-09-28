package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func ProjectTools(bpSvc ports.ProjectCatalog) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_projects",
			Description: "Return all projects. A project defines the directory root that everything (tree, zones, paths) is based on.",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return map[string]any{"projects": bpSvc.ListProjects()}, nil
			},
		},
		{
			Name:        "get_project",
			Description: "Return one project by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "project_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				p := bpSvc.GetProject(id)
				if p == nil {
					return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
				}
				return map[string]any{"project": p}, nil
			},
		},
		{
			Name:        "create_project",
			Description: "Create a project with a name and root directory. The root is the base path for list_tree, list_matching_paths, and zones.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"name":{"type":"string","description":"Project name"},"root_dir":{"type":"string","description":"Root directory path"}},"required":["root_dir"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				rootDir := getString(args, "root_dir", "")
				if rootDir == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "root_dir is required"}
				}
				name := getString(args, "name", "")
				p, err := bpSvc.CreateProject(name, rootDir)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": p}, nil
			},
		},
		{
			Name:        "update_project",
			Description: "Update a project's name and/or root_dir.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"name":{"type":"string","description":"Project name"},"root_dir":{"type":"string","description":"Root directory path"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "project_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				name := getString(args, "name", "")
				rootDir := getString(args, "root_dir", "")
				p, err := bpSvc.UpdateProject(id, name, rootDir)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": p}, nil
			},
		},
		{
			Name:        "delete_project",
			Description: "Delete a project by id. All zones belonging to the project are also deleted.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"}},"required":["project_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "project_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
				}
				if err := bpSvc.DeleteProject(id); err != nil {
					return nil, err
				}
				return map[string]string{"deleted": id}, nil
			},
		},
		{
			Name:        "add_ignored_path",
			Description: "Add a file or directory path to the project's ignore list. Ignored paths are hidden from the tree view.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"path":{"type":"string","description":"Path to ignore"}},"required":["project_id","path"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				path := getString(args, "path", "")
				if projectID == "" || path == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id and path are required"}
				}
				p, err := bpSvc.AddIgnoredPath(projectID, path)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": p}, nil
			},
		},
		{
			Name:        "remove_ignored_path",
			Description: "Remove a path from the project's ignore list so it is shown again in the tree view.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"project_id":{"type":"string","description":"Project ID"},"path":{"type":"string","description":"Path to remove from ignore list"}},"required":["project_id","path"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				projectID := getString(args, "project_id", "")
				path := getString(args, "path", "")
				if projectID == "" || path == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id and path are required"}
				}
				p, err := bpSvc.RemoveIgnoredPath(projectID, path)
				if err != nil {
					return nil, err
				}
				return map[string]any{"project": p}, nil
			},
		},
	}
}
