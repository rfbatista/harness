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
	// reached.
	shellUnavailable = "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written"
)

// ShellHooks are the claude settings hooks of a session whose shell's edit
// endpoints are under shellURL (…/api/sessions/{id}/shell): PreToolUse
// checks Edit, Write, MultiEdit and NotebookEdit, and Bash for redirects
// into the shell; PostToolUse commits the edits. Interactive sessions get
// them in --settings, headless ones in the CLI's settings.
//
// Each command posts the hook's own input to the endpoint, which answers in
// claude's hook output format (?hook=). The shell's path goes along in
// X-Harness-Shell so a Bash command naming $HARNESS_SHELL is read as the
// shell would read it. A harness that cannot be reached, or that fails,
// denies the tool before it runs, and blocks after it ran, with
// SHELL_UNAVAILABLE, so no write is silently lost.
func ShellHooks(shellURL string) map[string][]ports.HookGroup {
	post := func(endpoint, event, refusal string) []ports.HookCommand {
		cmd := "curl -fsS -m 5 -X POST -H 'Content-Type: application/json' -H \"X-Harness-Shell: $HARNESS_SHELL\" --data-binary @- '" +
			shellURL + "/" + endpoint + "?hook=" + event + "' 2>/dev/null || printf '%s\\n' '" + refusal + "'"
		return []ports.HookCommand{{Type: "command", Command: cmd}}
	}
	check := post("check", "PreToolUse",
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"`+shellUnavailable+`"}}`)
	commit := post("commit", "PostToolUse", `{"decision":"block","reason":"`+shellUnavailable+`"}`)
	return map[string][]ports.HookGroup{
		"PreToolUse": {
			{Matcher: ShellEditTools, Hooks: check},
			{Matcher: "Bash", Hooks: check},
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
