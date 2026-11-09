package orchestration

import (
	"strconv"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// The branch gate's asking hook and instruction (ADR 0069, contract "Harness
// server ↔ Agent sessions — branch gate" §2 and §6).

// BranchGateTools are the tools whose command the branch gate reads.
const BranchGateTools = "Bash"

// defaultBranchGateTimeout is the hook's timeout when the service has no
// approval timeout: the server's own default.
const defaultBranchGateTimeout = 24 * time.Hour

// BranchGateHooks are the claude settings hooks of a session whose branch gate
// is under gateURL (…/api/sessions/{id}/branch_gate): PreToolUse on Bash posts
// the hook's own input, and the endpoint holds its answer until the person
// answers. The timeout is the approval timeout, so none denies before the
// person has. A harness that cannot be reached answers nothing: the command
// goes on to the git shim, which refuses the branch action. The hook never
// decides on its own.
func BranchGateHooks(gateURL string, timeout time.Duration) map[claude.HookEvent][]claude.HookMatcher {
	if timeout <= 0 {
		timeout = defaultBranchGateTimeout
	}
	seconds := int(timeout.Seconds())
	curl := "curl -fsS -m " + strconv.Itoa(seconds) + " -X POST -H 'Content-Type: application/json'" +
		" --data-binary @- '" + gateURL + "?hook=PreToolUse' 2>/dev/null || true"
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse: {{Matcher: BranchGateTools, Hooks: []claude.Hook{{Type: "command", Command: curl, Timeout: seconds}}}},
	}
}

// BranchGateInstructionPath is the memory file every agent session gets.
const BranchGateInstructionPath = "BRANCH_GATE.md"

const branchGateInstruction = `# Branches

- Stay on your session branch. Do not create, switch, rename or delete branches.
- Ask for another branch only when it is really needed. Run the git command as its own Bash command, so the harness can ask the person. It waits until the person answers.
- Every branch action waits for the person's approval, and an approval covers that one action only. If the person denies it, their note says why.
- A push waits for the person's approval too, whatever the branch or the remote, your session branch included (ADR 0071).
- Force pushes and remote branch deletions are refused.
`

// branchGateInstructionFile is the instruction every agent session gets,
// delivered like the repository's instruction files (ADR 0057).
func branchGateInstructionFile() llmkit.InstructionFile {
	return llmkit.InstructionFile{Kind: llmkit.InstructionMemory, Path: BranchGateInstructionPath, Content: []byte(branchGateInstruction)}
}
