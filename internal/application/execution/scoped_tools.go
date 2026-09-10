package execution

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"operators-mcp/internal/domain"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// ScopedFilesystemTools returns Genkit ai.Tool wrappers around the provided
// filesystem tools with path validation enforcing the execution boundary.
// The raw tools are passed in to avoid an import cycle with the tooling package.
func ScopedFilesystemTools(g *genkit.Genkit, ec *ExecutionContext, raw []domain.Tool) []ai.ToolRef {
	out := make([]ai.ToolRef, 0, len(raw))

	for _, t := range raw {
		tool := t
		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}

		wrapped := genkit.DefineTool(g, "scoped_"+tool.Name,
			tool.Description,
			func(ctx *ai.ToolContext, input any) (any, error) {
				log := slog.With("tool", "scoped_"+tool.Name)
				log.Info("tool invoked", "input", input)

				args, _ := input.(map[string]any)
				if args == nil {
					args = make(map[string]any)
				}

				if err := validatePath(args, ec); err != nil {
					log.Warn("path validation rejected", "error", err)
					return nil, err
				}

				result, err := tool.Handler(ctx, args)
				if err != nil {
					log.Error("tool execution failed", "error", err)
					return nil, err
				}

				log.Info("tool execution succeeded", "result_type", fmt.Sprintf("%T", result))
				return result, nil
			},
			ai.WithInputSchema(schema),
		)
		out = append(out, wrapped)
	}
	return out
}

// validatePath checks every path-like argument in the tool input against the
// execution context boundaries: allowed root, ignored paths, zone pattern, and
// explicit paths.
func validatePath(args map[string]any, ec *ExecutionContext) error {
	pathArg := getStringArg(args, "path")
	if pathArg == "" {
		return nil
	}

	resolved := resolvePath(pathArg, ec.AllowedRoot)

	if !isUnderRoot(resolved, ec.AllowedRoot) {
		return &domain.StructuredError{
			Code:    "PATH_OUTSIDE_ROOT",
			Message: fmt.Sprintf("path %q is outside the project root %q", pathArg, ec.AllowedRoot),
		}
	}

	relPath, err := filepath.Rel(ec.AllowedRoot, resolved)
	if err != nil {
		return &domain.StructuredError{Code: "INVALID_PATH", Message: err.Error()}
	}

	for _, ignored := range ec.IgnoredPaths {
		if relPath == ignored || strings.HasPrefix(relPath, ignored+string(os.PathSeparator)) {
			return &domain.StructuredError{
				Code:    "PATH_IGNORED",
				Message: fmt.Sprintf("path %q is in the project's ignored list", relPath),
			}
		}
	}

	if !isPathAllowedByZone(relPath, ec.ZonePattern, ec.ExplicitPaths) {
		return &domain.StructuredError{
			Code:    "PATH_OUTSIDE_ZONE",
			Message: fmt.Sprintf("path %q is outside the zone boundaries", relPath),
		}
	}

	args["path"] = resolved

	return nil
}

func resolvePath(p, root string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(root, p))
}

func isUnderRoot(absPath, root string) bool {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

func isPathAllowedByZone(relPath, pattern string, explicitPaths []string) bool {
	if pattern == "" && len(explicitPaths) == 0 {
		return true
	}

	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err == nil && re.MatchString(relPath) {
			return true
		}
	}

	for _, ep := range explicitPaths {
		if relPath == ep || strings.HasPrefix(relPath, ep+string(os.PathSeparator)) {
			return true
		}
	}

	return false
}

func getStringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
