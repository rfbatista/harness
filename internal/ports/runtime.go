package ports

import (
	"context"
	"io"
	"os"

	"operators-mcp/internal/domain"
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

// Launcher starts one kind of agent on a terminal itself, where an Agent
// only builds the command a Shell and a PTY then run: claude, which llmkit
// starts (ADR 0007). shell still prepares the process.
type Launcher interface {
	// Kind is the AgentSpec.Kind this launcher serves, e.g. "claude".
	Kind() string
	// Launch starts spec on a terminal of size.
	Launch(ctx context.Context, spec AgentSpec, size TermSize, shell Shell) (PTYProcess, error)
}

// AgentSpec is everything the server resolved for one interactive session:
// what to run and with which agent configuration. It crosses the wire, so the
// process can be started wherever the session runs.
type AgentSpec struct {
	// Kind picks the Agent adapter: "claude", or "command" for a plain shell
	// command (an application run).
	Kind string `json:"kind"`
	// Command is the shell command line a "command" agent runs.
	Command   string `json:"command,omitempty"`
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
	// Hooks are more claude settings hooks by event (PreToolUse, ...),
	// added beside the ones HookURL brings: a task session's edit proxy.
	Hooks map[string][]HookGroup `json:"hooks,omitempty"`
	// Prompt is the optional first message.
	Prompt string   `json:"prompt,omitempty"`
	Env    []string `json:"env,omitempty"`
	// EnvFile is an sh file a login shell sources after the person's
	// profile, so the profile cannot override Env's git credentials (see
	// "Contract: Harness server ↔ Agent sessions — git credentials").
	EnvFile      string       `json:"env_file,omitempty"`
	Conversation Conversation `json:"conversation"`
}

// HookGroup is one entry of a claude settings hook event: the commands run
// for the tools Matcher names (a regular expression; empty for all).
type HookGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand is a shell command claude runs as a hook, with the hook's
// input on its stdin.
type HookCommand struct {
	Type    string `json:"type"` // "command"
	Command string `json:"command"`
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
	// EnvFile, when set, is sourced by a login shell after the profile and
	// before the program runs.
	EnvFile string
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

// AppRunner runs a repository's application from a session's worktree, in a
// terminal on the server, and keeps each run's output so it can be read
// again. Runs live as long as the server does.
type AppRunner interface {
	// Start runs the session's repository's saved command called name, or,
	// with name empty, the command line given. Starting a saved command that
	// is already running for the session returns that run.
	Start(ctx context.Context, sessionID, name, command string) (*domain.AppRun, error)
	// List returns the session's runs, newest first.
	List(ctx context.Context, sessionID string) ([]*domain.AppRun, error)
	// Stop ends a run; RUN_NOT_FOUND when there is none.
	Stop(ctx context.Context, runID string) (*domain.AppRun, error)
	// Attach returns the run's terminal; its Subscribe replays the run's
	// output so far before the live stream.
	Attach(ctx context.Context, runID string) (Terminal, error)
}
