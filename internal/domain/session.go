package domain

import "time"

// SessionStatus is the lifecycle state of a running Claude CLI session.
type SessionStatus string

const (
	SessionStarting SessionStatus = "starting"
	SessionRunning  SessionStatus = "running"
	// SessionIdle is the turn boundary: the CLI process is alive and has
	// finished its turn, so it is the user's move. It is what distinguishes an
	// interactive session from a one-shot run.
	SessionIdle            SessionStatus = "idle"
	SessionThinking        SessionStatus = "thinking"
	SessionWaitingApproval SessionStatus = "waiting_approval"
	SessionPaused          SessionStatus = "paused"
	SessionDone            SessionStatus = "done"
	SessionFailed          SessionStatus = "failed"
	SessionStopped         SessionStatus = "stopped"
)

// IsTerminal reports whether the process behind the session is gone for good.
// A terminal session takes no further input and owes the user no decisions.
func (s SessionStatus) IsTerminal() bool {
	switch s {
	case SessionDone, SessionFailed, SessionStopped:
		return true
	default:
		return false
	}
}

// Session is a live Claude CLI run. The Agent (template) and Zone are referenced
// by ID; WorkingDir is the project root the process runs in.
type Session struct {
	ID               string        `json:"id"`
	ProjectID        string        `json:"project_id"`
	RepositoryID     string        `json:"repository_id,omitempty"`
	AgentID          string        `json:"agent_id,omitempty"`
	ZoneID           string        `json:"zone_id,omitempty"`
	TicketID         string        `json:"ticket_id,omitempty"`
	Task             string        `json:"task"`
	WorkingDir       string        `json:"working_dir"`
	Model            string        `json:"model,omitempty"`
	Status           SessionStatus `json:"status"`
	CostUSD          float64       `json:"cost_usd"`
	InputTokens      int           `json:"input_tokens"`
	OutputTokens     int           `json:"output_tokens"`
	LastAction       string        `json:"last_action,omitempty"`
	PendingApprovals int           `json:"pending_approvals"`
	// AutoRun means the session answers its own permission requests: tool calls
	// run without asking. It is switched per session while it runs, and it never
	// covers AskUserQuestion — a question needs the user's actual answer.
	AutoRun bool `json:"auto_run"`
	// WorkspaceID is the git worktree the session runs in; Branch is the branch
	// that worktree holds. Both are empty for sessions created before worktree
	// isolation.
	WorkspaceID string `json:"workspace_id,omitempty"`
	Branch      string `json:"branch,omitempty"`
	// Interactive sessions run claude in a terminal a person types into,
	// rather than being driven by the server over stream-json: the server
	// cannot send to or approve for them.
	Interactive bool `json:"interactive"`
	// RunsOn is where an interactive session's agent runs; RunnerHost names
	// the machine of a RunnerTUI session. Both are empty for headless ones.
	RunsOn     Runner `json:"runs_on,omitempty"`
	RunnerHost string `json:"runner_host,omitempty"`
	// ParentSessionID is the session that started this one on its task
	// (through the start_task_session tool); empty when a person did.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// Mode is what the session was started to do beyond its agent; it brings
	// its own skills, and they come back when the session is resumed.
	Mode SessionMode `json:"mode,omitempty"`
	// ClaudeSessionID is the claude conversation an interactive session resumes.
	// It starts equal to ID (the CLI is launched with --session-id ID) and moves
	// when the conversation does, e.g. after /clear; the CLI's SessionStart hook
	// reports it.
	ClaudeSessionID string    `json:"claude_session_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Runner is where an interactive session's agent process runs.
type Runner string

const (
	// RunnerServer sessions run on the server's terminal host: they outlive
	// the client that started them, and any client can attach to them.
	RunnerServer Runner = "server"
	// RunnerTUI sessions run inside the tui-client that started them and end
	// with it; the server only records them.
	RunnerTUI Runner = "tui"
)

// Valid reports whether r names a runner.
func (r Runner) Valid() bool { return r == RunnerServer || r == RunnerTUI }

// SessionMode is a way of starting a task session that layers a role on top
// of whichever agent it runs: its own skills and its own first message.
type SessionMode string

const (
	// SessionModeDefault is a session that is only its agent.
	SessionModeDefault SessionMode = ""
	// SessionModeArchitect shapes the task into per-application specs and
	// contracts, kept as task documents, and delegates each to a planning agent.
	SessionModeArchitect SessionMode = "architect"
)

// ParseSessionMode reads a mode as clients send it; empty is the default.
func ParseSessionMode(s string) (SessionMode, error) {
	switch m := SessionMode(s); m {
	case SessionModeDefault, SessionModeArchitect:
		return m, nil
	default:
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "unknown session mode " + s + `: use "" or "architect"`}
	}
}
