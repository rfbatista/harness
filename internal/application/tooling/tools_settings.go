package tooling

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SettingsTools exposes the global key/value app settings.
func SettingsTools(bpSvc ports.SettingsEditor) []domain.Tool {
	return []domain.Tool{
		{
			Name:        "get_settings",
			Description: "Return all global app settings, including skills.publish_root (the directory published skills are written to).",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				settings, err := bpSvc.GetSettings()
				if err != nil {
					return nil, err
				}
				return map[string]any{"settings": settings}, nil
			},
		},
		{
			Name:        "update_settings",
			Description: "Merge keys into the global app settings. Changing skills.publish_root republishes every published skill under the new root.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"settings":{"type":"object","additionalProperties":{"type":"string"},"description":"keys to set, e.g. {\"skills.publish_root\":\"/Users/me/.claude/skills\"}"}},"required":["settings"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				values := getStringMap(args, "settings")
				if len(values) == 0 {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "settings is required"}
				}
				settings, err := bpSvc.UpdateSettings(values)
				if err != nil {
					return nil, err
				}
				return map[string]any{"settings": settings}, nil
			},
		},
	}
}
