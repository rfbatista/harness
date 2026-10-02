// Package claudecli is the ports.Agent for the interactive claude CLI: it
// turns an AgentSpec into claude's command line.
package claudecli

import (
	"encoding/json"
	"fmt"
	"strings"

	"operators-mcp/internal/ports"
)

// Kind is the AgentSpec.Kind this adapter serves.
const Kind = "claude"

var _ ports.Agent = Agent{}

// Agent builds claude invocations.
type Agent struct {
	// Bin is the claude binary; empty means "claude", found by the shell.
	Bin string
}

// New returns the claude agent running bin.
func New(bin string) Agent { return Agent{Bin: bin} }

func (Agent) Kind() string { return Kind }

// Command is the interactive CLI's argv for spec. It carries what the
// headless argv does (llmkit's, which is unexported and hardwired to --print
// and stream-json) minus the protocol flags and the approval server: in a
// terminal claude asks its questions itself. It adds the SessionStart hook
// that reports conversation changes back to the server.
func (a Agent) Command(spec ports.AgentSpec) (ports.ShellCommand, error) {
	if spec.Kind != "" && spec.Kind != Kind {
		return ports.ShellCommand{}, fmt.Errorf("claudecli: cannot run a %q agent", spec.Kind)
	}
	if spec.Conversation.ID == "" {
		return ports.ShellCommand{}, fmt.Errorf("claudecli: no conversation id")
	}
	var args []string
	if spec.Conversation.Resume {
		args = append(args, "--resume", spec.Conversation.ID)
	} else {
		args = append(args, "--session-id", spec.Conversation.ID)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	// --add-dir and --allowedTools take a variable number of values; one
	// flag per dir and a comma-joined tool list keep them from swallowing
	// the prompt.
	for _, d := range spec.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if len(spec.MCPServers) > 0 {
		mcp, err := mcpConfigJSON(spec.MCPServers)
		if err != nil {
			return ports.ShellCommand{}, err
		}
		args = append(args, "--mcp-config", mcp)
	}
	if len(spec.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(spec.AllowedTools, ","))
	}
	switch spec.Permission {
	case "accept_edits":
		args = append(args, "--permission-mode", "acceptEdits")
	case "bypass":
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	for _, d := range spec.SkillDirs {
		args = append(args, "--plugin-dir", d)
	}
	if spec.AppendSystem != "" {
		args = append(args, "--append-system-prompt", spec.AppendSystem)
	}
	if spec.HookURL != "" {
		settings, err := sessionStartHookSettings(spec.HookURL)
		if err != nil {
			return ports.ShellCommand{}, err
		}
		args = append(args, "--settings", settings)
	}
	if spec.Prompt != "" {
		args = append(args, "--", spec.Prompt)
	}
	bin := a.Bin
	if bin == "" {
		bin = "claude"
	}
	return ports.ShellCommand{Program: bin, Args: args, Dir: spec.Dir, Env: spec.Env}, nil
}

// mcpConfigJSON is the --mcp-config payload for servers, in the shape llmkit
// writes for headless sessions (its builder is unexported, and also adds the
// approval server interactive sessions must not have).
func mcpConfigJSON(servers []ports.MCPServerSpec) (string, error) {
	out := map[string]any{}
	for _, sv := range servers {
		entry := map[string]any{}
		switch sv.Transport {
		case "http", "streamable-http", "sse":
			entry["type"] = "http"
			entry["url"] = sv.URL
			if len(sv.Headers) > 0 {
				entry["headers"] = sv.Headers
			}
		default: // stdio
			entry["command"] = sv.Command
			if len(sv.Args) > 0 {
				entry["args"] = sv.Args
			}
			if len(sv.Env) > 0 {
				entry["env"] = sv.Env
			}
		}
		out[sv.Name] = entry
	}
	b, err := json.Marshal(map[string]any{"mcpServers": out})
	return string(b), err
}

// sessionStartHookSettings is a --settings payload whose SessionStart hook
// posts the hook's input — which names the current conversation — to url.
// --settings layers over the user's own settings rather than replacing them.
//
// The hook must never disturb the session: its stdout would be added to
// claude's context, so the response is discarded, and a server that is down
// only costs the five-second timeout.
func sessionStartHookSettings(url string) (string, error) {
	cmd := "curl -fsS -m 5 -X POST -H 'Content-Type: application/json' --data-binary @- '" +
		url + "' >/dev/null 2>&1 || true"
	b, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd}}},
			},
		},
	})
	return string(b), err
}
