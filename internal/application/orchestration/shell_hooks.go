package orchestration

import (
	"slices"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/ports"
)

// specHooks are a claude settings' hooks as an AgentSpec carries them to a
// terminal host.
func specHooks(settings *claude.Settings) map[string][]ports.HookGroup {
	if settings == nil || len(settings.Hooks) == 0 {
		return nil
	}
	out := make(map[string][]ports.HookGroup, len(settings.Hooks))
	for event, matchers := range settings.Hooks {
		for _, m := range matchers {
			g := ports.HookGroup{Matcher: m.Matcher}
			for _, h := range m.Hooks {
				g.Hooks = append(g.Hooks, ports.HookCommand{Type: h.Type, Command: h.Command, Timeout: h.Timeout})
			}
			out[string(event)] = append(out[string(event)], g)
		}
	}
	return out
}

// withHooks is driver with hooks added to its claude settings, next to the
// hooks and other settings it already has. A nil driver becomes a claude
// config carrying only them; any other driver is not claude's, and is
// answered false.
func withHooks(driver llmkit.DriverConfig, hooks map[claude.HookEvent][]claude.HookMatcher) (claude.Config, bool) {
	var dc claude.Config
	switch d := driver.(type) {
	case nil:
	case claude.Config:
		dc = d
	default:
		return claude.Config{}, false
	}
	var settings claude.Settings
	if dc.Settings != nil {
		settings = *dc.Settings
	}
	merged := make(map[claude.HookEvent][]claude.HookMatcher, len(settings.Hooks)+len(hooks))
	for event, matchers := range settings.Hooks {
		merged[event] = slices.Clone(matchers)
	}
	for event, matchers := range hooks {
		merged[event] = append(merged[event], matchers...)
	}
	settings.Hooks = merged
	dc.Settings = &settings
	return dc, true
}
