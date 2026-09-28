package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func PromptTools(bpSvc ports.PromptCatalog) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "list_prompts",
			Description: "Return all prompts.",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return map[string]any{"prompts": bpSvc.ListPrompts()}, nil
			},
		},
		{
			Name:        "get_prompt",
			Description: "Return one prompt by id.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"prompt_id":{"type":"string","description":"Prompt ID"}},"required":["prompt_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "prompt_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt_id is required"}
				}
				p := bpSvc.GetPrompt(id)
				if p == nil {
					return nil, &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
				}
				return map[string]any{"prompt": p}, nil
			},
		},
		{
			Name:        "create_prompt",
			Description: "Create a prompt with a name, description, and content.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"name":{"type":"string","description":"Prompt name"},"description":{"type":"string","description":"Prompt description"},"content":{"type":"string","description":"Prompt content text"}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				name := getString(args, "name", "")
				description := getString(args, "description", "")
				content := getString(args, "content", "")
				p, err := bpSvc.CreatePrompt(name, description, content)
				if err != nil {
					return nil, err
				}
				return map[string]any{"prompt": p}, nil
			},
		},
		{
			Name:        "update_prompt",
			Description: "Update a prompt's name, description, and/or content.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"prompt_id":{"type":"string","description":"Prompt ID"},"name":{"type":"string","description":"Prompt name"},"description":{"type":"string","description":"Prompt description"},"content":{"type":"string","description":"Prompt content text"}},"required":["prompt_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "prompt_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt_id is required"}
				}
				name := getString(args, "name", "")
				description := getString(args, "description", "")
				content := getString(args, "content", "")
				p, err := bpSvc.UpdatePrompt(id, name, description, content)
				if err != nil {
					return nil, err
				}
				return map[string]any{"prompt": p}, nil
			},
		},
		{
			Name:        "delete_prompt",
			Description: "Delete a prompt by id. Agents referencing it will have their prompt unlinked.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"prompt_id":{"type":"string","description":"Prompt ID"}},"required":["prompt_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				id := getString(args, "prompt_id", "")
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt_id is required"}
				}
				if err := bpSvc.DeletePrompt(id); err != nil {
					return nil, err
				}
				return map[string]string{"deleted": id}, nil
			},
		},
	}
}
