package orchestration

import (
	"github.com/rfbatista/llmkit/claude"
)

// The architecture rules' hooks (ADR 0033). Their matchers differ from the
// edit proxy's, so a session carries both and each answers on its own.
const (
	// ArchitectureBeforeTools are the tools whose use the rules check
	// before it runs: the file tools, and Bash for command rules.
	ArchitectureBeforeTools = "Edit|Write|MultiEdit|Bash"
	// ArchitectureAfterTools are the tools whose edit the rules look at once
	// it landed.
	ArchitectureAfterTools = "Edit|Write|MultiEdit"
	// ArchitectureUnreachable is what the agent is told when the harness
	// cannot be reached: the check never blocks on its own failure.
	ArchitectureUnreachable = "Architecture rules could not be checked: the harness could not be reached."
)

// architectureHookTimeout is how long claude lets an architecture hook run:
// a command rule may take its longest timeout (300 s), and the call around it.
const architectureHookTimeout = 330

// ArchitectureHooks are the claude settings hooks of a session whose
// architecture endpoints are under archURL (…/api/sessions/{id}/architecture):
// PreToolUse checks the file tools and Bash, PostToolUse the edits. Each posts
// the hook's own input and the endpoint answers in claude's hook output
// format. A harness that cannot be reached lets the action through and says so
// as context.
func ArchitectureHooks(archURL string) map[claude.HookEvent][]claude.HookMatcher {
	hook := func(event string) []claude.Hook {
		curl := "curl -fsS -m 330 -X POST -H 'Content-Type: application/json'" +
			" --data-binary @- '" + archURL + "/check?hook=" + event + "' 2>/dev/null"
		down := `{"hookSpecificOutput":{"hookEventName":"` + event + `","additionalContext":"` + ArchitectureUnreachable + `"}}`
		return []claude.Hook{{Type: "command", Command: curl + " || printf '%s\\n' '" + down + "'", Timeout: architectureHookTimeout}}
	}
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse:  {{Matcher: ArchitectureBeforeTools, Hooks: hook("PreToolUse")}},
		claude.HookPostToolUse: {{Matcher: ArchitectureAfterTools, Hooks: hook("PostToolUse")}},
	}
}
