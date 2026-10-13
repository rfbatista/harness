package orchestration

import (
	"strings"

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
// them in --settings, headless ones in the CLI's settings.
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
func ShellHooks(shellURL string) map[string][]ports.HookGroup {
	curl := func(endpoint, event string) string {
		return "curl -fsS -m 5 -X POST -H 'Content-Type: application/json'" +
			` -H "X-Harness-Shell: $HARNESS_SHELL" -H "X-Harness-Home: $HOME"` +
			" --data-binary @- '" + shellURL + "/" + endpoint + "?hook=" + event + "' 2>/dev/null"
	}
	hook := func(cmd string) []ports.HookCommand { return []ports.HookCommand{{Type: "command", Command: cmd}} }
	check := hook(curl("check", "PreToolUse") + " || printf '%s\\n' '" +
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"` + shellUnavailable + `"}}'`)
	bash := hook(curl("check", "PreToolUse") + " || true")
	// The input is read once: curl gets it, and if the harness is down the
	// file's path is taken from it (file_path, or notebook_path), relative
	// to the shell as every refusal names it.
	commit := hook(`in=$(cat); printf '%s' "$in" | ` + curl("commit", "PostToolUse") +
		` || { p=$(printf '%s' "$in" | sed -nE 's/.*"(file_path|notebook_path)"[[:space:]]*:[[:space:]]*"([^"]*)".*/\2/p' | head -n 1); p=${p#"$HARNESS_SHELL"/};` +
		` printf '{"decision":"block","reason":"SHELL_UNAVAILABLE: the harness could not be reached; %s changed on disk but was not stored — redo the change once the harness is back"}\n' "$p"; }`)
	return map[string][]ports.HookGroup{
		"PreToolUse": {
			{Matcher: ShellEditTools, Hooks: check},
			{Matcher: "Bash", Hooks: bash},
		},
		"PostToolUse": {
			{Matcher: ShellEditTools, Hooks: commit},
		},
	}
}

// shellDir is the session's virtual shell, from its environment; empty when
// it has none.
func shellDir(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "HARNESS_SHELL="); ok {
			return v
		}
	}
	return ""
}
