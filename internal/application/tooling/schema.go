package tooling

import "github.com/rfbatista/harnesskit/tool"

// The schema and argument helpers live in harnesskit/tool. These wrappers keep
// the local tool groups reading the way they always have.

func schemaFromJSON(raw string) map[string]any { return tool.SchemaFromJSON(raw) }

func emptySchema() map[string]any { return tool.EmptySchema() }

func getString(args map[string]any, key, def string) string { return tool.String(args, key, def) }

func getInt(args map[string]any, key string, def int) int { return tool.Int(args, key, def) }

func getBool(args map[string]any, key string, def bool) bool { return tool.Bool(args, key, def) }

func getFloat(args map[string]any, key string) (float64, bool) { return tool.Float(args, key) }

func getStringSlice(args map[string]any, key string) []string { return tool.StringSlice(args, key) }

func getStringSliceDefault(args map[string]any, key string, def []string) []string {
	return tool.StringSliceOr(args, key, def)
}

// getStringMap collapses an empty result to nil; extractStringMap keeps it.
// The difference decides whether a partial update clears a stored map or leaves
// it alone, so the two must stay distinct.
func getStringMap(args map[string]any, key string) map[string]string {
	return tool.StringMap(args, key)
}

func extractStringMap(args map[string]any, key string) map[string]string {
	return tool.StringMapRaw(args, key)
}

func extractAnyMap(args map[string]any, key string) map[string]any { return tool.AnyMap(args, key) }
