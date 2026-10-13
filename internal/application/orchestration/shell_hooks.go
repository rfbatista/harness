package orchestration

import (
	"slices"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/ports"
)

// The edit proxy's hooks (contract "Harness server ↔ Agent sessions —
// virtual shell" §5).
const (
	// ShellEditTools are the claude tools that write files.
	ShellEditTools = "Edit|Write|MultiEdit|NotebookEdit"
	// shellUnavailable is the contract's refusal when the harness cannot be
	// reached before an edit tool runs.
	shellUnavailable = "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written"
)

// ShellHooks are the claude settings hooks of a session whose shell's edit
// endpoints are under shellURL (…/api/sessions/{id}/shell): PreToolUse
// checks Edit, Write, MultiEdit and NotebookEdit, and Bash for redirects
// into the shell; PostToolUse commits the edits. Interactive sessions get
// them in --settings through their AgentSpec (agentSpec), headless ones in
// claude.Config.Settings, which llmkit hands the CLI as --settings.
//
// Each command posts the hook's own input to the endpoint, which answers in
// claude's hook output format (?hook=). The shell's path and the session's
// home go along in X-Harness-Shell and X-Harness-Home so a Bash command
// naming $HARNESS_SHELL, $HOME or ~ is read as the shell would read it.
//
// A harness that cannot be reached, or that fails, denies an edit tool
// before it runs with SHELL_UNAVAILABLE, and after it ran blocks naming the
// file that changed on disk but was not stored, so no write is silently
// lost. Bash goes on: its redirects are never stored, and the README says
// so, while blocking every command would stall the session.
func ShellHooks(shellURL string) map[claude.HookEvent][]claude.HookMatcher {
	curl := func(endpoint, event string) string {
		return "curl -fsS -m 5 -X POST -H 'Content-Type: application/json'" +
			` -H "X-Harness-Shell: $HARNESS_SHELL" -H "X-Harness-Home: $HOME"` +
			" --data-binary @- '" + shellURL + "/" + endpoint + "?hook=" + event + "' 2>/dev/null"
	}
	hook := func(cmd string) []claude.Hook { return []claude.Hook{{Type: "command", Command: cmd}} }
	check := hook(curl("check", "PreToolUse") + " || printf '%s\\n' '" +
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"` + shellUnavailable + `"}}'`)
	bash := hook(curl("check", "PreToolUse") + " || true")
	// The input is read once: curl gets it, and if the harness is down the
	// file's path is taken from it (file_path, or notebook_path), relative
	// to the shell as every refusal names it.
	commit := hook(`in=$(cat); printf '%s' "$in" | ` + curl("commit", "PostToolUse") +
		` || { p=$(printf '%s' "$in" | sed -nE 's/.*"(file_path|notebook_path)"[[:space:]]*:[[:space:]]*"([^"]*)".*/\2/p' | head -n 1); p=${p#"$HARNESS_SHELL"/};` +
		` printf '{"decision":"block","reason":"SHELL_UNAVAILABLE: the harness could not be reached; %s changed on disk but was not stored — redo the change once the harness is back"}\n' "$p"; }`)
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse: {
			{Matcher: ShellEditTools, Hooks: check},
			{Matcher: "Bash", Hooks: bash},
		},
		claude.HookPostToolUse: {
			{Matcher: ShellEditTools, Hooks: commit},
		},
	}
}

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
				g.Hooks = append(g.Hooks, ports.HookCommand{Type: h.Type, Command: h.Command})
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
