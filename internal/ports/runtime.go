package ports

import (
	"context"
	"io"
	"os"
)

// The runtime stack an interactive agent runs on, driven ports composed by a
// TerminalHost: an Agent turns a session into a command, a Shell turns the
// command into a process for its environment, and a PTY runs the process on
// a terminal device. Each is swapped on its own — another agent CLI, a login
// shell or a container, another kind of terminal.

// Agent builds the command line of one kind of agent CLI.
type Agent interface {
	// Kind is the AgentSpec.Kind this adapter serves, e.g. "claude".
	Kind() string
	// Command is the CLI invocation that runs spec.
	Command(spec AgentSpec) (ShellCommand, error)
}

// AgentSpec is everything the server resolved for one interactive session:
// what to run and with which agent configuration. It crosses the wire, so the
// process can be started wherever the session runs.
type AgentSpec struct {
	// Kind picks the Agent adapter; "claude" is the one there is.
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	// Dir is the session's worktree.
	Dir   string `json:"dir"`
	Model string `json:"model,omitempty"`
	// Permission is "ask" (or empty), "accept_edits" or "bypass".
	Permission   string          `json:"permission,omitempty"`
	AppendSystem string          `json:"append_system,omitempty"`
	MCPServers   []MCPServerSpec `json:"mcp_servers,omitempty"`
	AllowedTools []string        `json:"allowed_tools,omitempty"`
	AddDirs      []string        `json:"add_dirs,omitempty"`
	// SkillDirs are plugin directories carrying the agent's skills.
	SkillDirs []string `json:"skill_dirs,omitempty"`
	// HookURL, when set, is where the CLI reports the conversation it is in
	// (claude's SessionStart hook).
	HookURL string `json:"hook_url,omitempty"`
	// Prompt is the optional first message.
	Prompt       string       `json:"prompt,omitempty"`
	Env          []string     `json:"env,omitempty"`
	Conversation Conversation `json:"conversation"`
}

// MCPServerSpec is an MCP server attached to a session.
type MCPServerSpec struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport,omitempty"` // "stdio" (default) or "http"
	Command   string            `json:"command,omitempty"`
	URL       string            `json:"url,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// Conversation is the agent's own conversation a session opens: a new one
// under ID, or ID resumed.
type Conversation struct {
	ID     string `json:"id"`
	Resume bool   `json:"resume,omitempty"`
}

// ShellCommand is an agent CLI invocation, before a Shell decides how and
// where it runs.
type ShellCommand struct {
	// Program is the CLI, a name the shell resolves or a path.
	Program string
	Args    []string
	// Dir is a path in the shell's environment.
	Dir string
	// Env is added to the shell's own environment.
	Env []string
}

// Shell decides what process runs a command, and where: this machine's login
// shell, a container, a remote host.
type Shell interface {
	// Prepare turns cmd into the local process to start.
	Prepare(ctx context.Context, cmd ShellCommand) (ProcessSpec, error)
	// CheckDir reports WORKSPACE_MISSING unless dir exists in the shell's
	// environment.
	CheckDir(ctx context.Context, dir string) error
}

// ProcessSpec is a concrete local process.
type ProcessSpec struct {
	Path string
	Args []string // not including Path
	Dir  string
	// Env is the complete environment.
	Env []string
}

// TermSize is a terminal's size in cells.
type TermSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// PTY runs a process on a pseudo-terminal.
type PTY interface {
	Start(spec ProcessSpec, size TermSize) (PTYProcess, error)
}

// PTYProcess is a process on a terminal: reading gives what it prints,
// writing is what it reads as typed input.
type PTYProcess interface {
	io.Reader
	io.Writer
	Resize(TermSize) error
	Signal(os.Signal) error
	// Wait reaps the process and returns its exit code; err is only for a
	// failure to wait, never for a non-zero exit.
	Wait() (exitCode int, err error)
	// Close releases the terminal device.
	Close() error
}
