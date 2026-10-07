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
// terminal claude asks its questions itself. It adds the hooks that report
// back to the server: the conversation it is in, and where its turns start
// and end.
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
		settings, err := sessionHookSettings(spec.HookURL)
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

// sessionHookSettings is a --settings payload whose hooks post their input
// to url, with the hook's name added as event=: SessionStart names the
// conversation, UserPromptSubmit starts a turn and Stop ends one.
// --settings layers over the user's own settings rather than replacing them.
//
// The hooks must never disturb the session. SessionStart's and
// UserPromptSubmit's stdout would be added to claude's context, so their
// response is discarded. Stop's is passed through: it is how the server hands
// claude turns that waited for this one to end ({"decision":"block",...}),
// and it is empty otherwise. A server that is down only costs the
// five-second timeout, and the session goes on as if it had answered nothing.
func sessionHookSettings(url string) (string, error) {
	sep := "&"
	if !strings.Contains(url, "?") {
		sep = "?"
	}
	post := func(event, out string) any {
		cmd := "curl -fsS -m 5 -X POST -H 'Content-Type: application/json' --data-binary @- '" +
			url + sep + "event=" + event + "' " + out + " || true"
		return []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd}}}}
	}
	b, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"SessionStart":     post("SessionStart", ">/dev/null 2>&1"),
			"UserPromptSubmit": post("UserPromptSubmit", ">/dev/null 2>&1"),
			"Stop":             post("Stop", "2>/dev/null"),
		},
	})
	return string(b), err
}
