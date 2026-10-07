package ports

import (
	"context"
	"encoding/json"
	"time"

	"operators-mcp/internal/domain"
)

// Driving ports of the orchestration service: how inbound adapters start,
// drive and watch agent sessions. *orchestration.Service satisfies them all.

// SessionLauncher starts headless sessions, driven by the server over the
// CLI's stream-json protocol.
type SessionLauncher interface {
	Start(ctx context.Context, req StartRequest) (*domain.Session, error)
}

// SessionControl acts on a headless session while it runs, and deletes any
// session. The live operations answer SESSION_INTERACTIVE for a session that
// runs in a terminal.
type SessionControl interface {
	Send(ctx context.Context, id, text string) error
	Resolve(ctx context.Context, id, reqID string, allow bool, msg string) error
	Answer(ctx context.Context, id, reqID string, answers, notes map[string]string) error
	SetAutoRun(ctx context.Context, id string, autoRun bool) error
	Stop(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// SessionReader reads recorded sessions. It is network-safe: tui-client
// implements it over HTTP, so a missing session is SESSION_NOT_FOUND.
type SessionReader interface {
	Get(ctx context.Context, id string) (*domain.Session, error)
	List(ctx context.Context, f SessionFilter) ([]*domain.Session, error)
}

// SessionStream follows a session's events: live, and replayed from the log.
type SessionStream interface {
	// Subscribe returns the live channel, the events already buffered, and a
	// function that ends the subscription.
	Subscribe(id string) (<-chan SessionEvent, []SessionEvent, func())
	History(id string, fromSeq int64) []SessionEvent
}

// SessionFeed follows a project as it changes: its sessions (started, ended,
// stopped, deleted, waiting on an approval) and its tickets (created, updated,
// deleted, from any entry point). It is network-safe: tui-client follows it
// over server-sent events and keeps only the session changes.
type SessionFeed interface {
	// FollowProject delivers each change in the project until ctx ends, then
	// closes the channel. It also closes early when the follower falls
	// behind: changes were dropped, so the follower reloads what it shows and
	// follows again.
	FollowProject(ctx context.Context, projectID string) (<-chan SessionChange, error)
}

// ProjectChange is one thing in a project as it is after a change, or a
// deleted one. Exactly one of the pointers is set; the wire tells them apart
// by which key is present, so the others must be absent, never null.
type ProjectChange struct {
	Session *domain.Session `json:"session,omitempty"`
	Ticket  *domain.Ticket  `json:"ticket,omitempty"`
	// The architect channel: a message created or delivered, a review request
	// created or settled, a status check created, changed or fired, a task's
	// status moved.
	TaskMessage   *domain.TaskMessage      `json:"task_message,omitempty"`
	ReviewRequest *domain.ReviewRequest    `json:"review_request,omitempty"`
	StatusCheck   *StatusCheckEvent        `json:"status_check,omitempty"`
	TaskStatus    *domain.TaskStatusChange `json:"task_status,omitempty"`
	Deleted       bool                     `json:"deleted,omitempty"`
}

// ProjectID is the project whose followers the change is for.
func (c ProjectChange) ProjectID() string {
	switch {
	case c.Session != nil:
		return c.Session.ProjectID
	case c.Ticket != nil:
		return c.Ticket.ProjectID
	case c.TaskMessage != nil:
		return c.TaskMessage.ProjectID
	case c.ReviewRequest != nil:
		return c.ReviewRequest.ProjectID
	case c.StatusCheck != nil:
		return c.StatusCheck.ProjectID
	case c.TaskStatus != nil:
		return c.TaskStatus.ProjectID
	}
	return ""
}

// SessionChange is the name the feed had when it carried sessions only. The
// TUI client still uses it; new code says ProjectChange.
type SessionChange = ProjectChange

// InteractiveSessions provisions and records sessions whose CLI runs in a
// terminal the user types into. The server provisions and records them and
// returns the AgentSpec to run. A RunnerServer session is spawned on the
// server's terminal host right away; a RunnerTUI one is the client's to run,
// and to report the end of with EndInteractive.
type InteractiveSessions interface {
	StartInteractive(ctx context.Context, req InteractiveRequest) (*domain.Session, AgentSpec, error)
	ResumeInteractive(ctx context.Context, req ResumeRequest) (*domain.Session, AgentSpec, error)
	// EndInteractive records that a RunnerTUI session's process ended. For a
	// RunnerServer session it answers SESSION_RUNS_ON_SERVER: the server
	// ends those itself.
	EndInteractive(ctx context.Context, id string, exitCode int, closedByUser bool) (*domain.Session, error)
}

// TerminalAccess attaches to the terminal of a RunnerServer session. It
// answers SESSION_RUNS_ON_TUI for a session the client runs, and
// SESSION_NOT_RUNNING once the session has ended.
type TerminalAccess interface {
	AttachTerminal(ctx context.Context, sessionID string) (Terminal, error)
}

// ConversationRecorder records which claude conversation an interactive
// session is in. The CLI's SessionStart hook drives it over a loopback-only
// route; clients never call it, so it is not part of InteractiveSessions.
type ConversationRecorder interface {
	RecordClaudeSession(ctx context.Context, id, claudeSessionID string) error
}

// TurnHooks follows an interactive session's turns through the CLI's
// UserPromptSubmit and Stop hooks, which drive them over a loopback-only
// route. A turn that ends while turns wait in the session's outbox goes on
// with them: TurnEnded returns the text the Stop hook hands back to claude
// to continue with ("" lets the session stop). stopHookActive is the hook's
// own flag: the turn is already a continuation.
type TurnHooks interface {
	TurnStarted(ctx context.Context, sessionID string) error
	TurnEnded(ctx context.Context, sessionID string, stopHookActive bool) (continueWith string, err error)
}

// Orchestration is the whole session surface, for an adapter that serves all
// of it (the HTTP API).
type Orchestration interface {
	SessionLauncher
	SessionControl
	SessionReader
	SessionStream
	SessionFeed
	InteractiveSessions
	ConversationRecorder
	TurnHooks
	TerminalAccess
}

// StartRequest starts a headless session.
type StartRequest struct {
	ProjectID     string   `json:"project_id"`
	RepositoryID  string   `json:"repository_id,omitempty"`
	AgentID       string   `json:"agent_id"`
	ZoneID        string   `json:"zone_id"`
	TicketID      string   `json:"ticket_id,omitempty"`
	Task          string   `json:"task"`
	ModelOverride string   `json:"model"`
	AllowedTools  []string `json:"allowed_tools"`
	AutoAccept    string   `json:"auto_accept"` // "off" | "edits" | "all"
	// BaseBranch is the ref the session's branch is cut from; empty means HEAD.
	BaseBranch string `json:"base_branch,omitempty"`
	// Branch is the branch created for the session; empty derives one from Task.
	Branch string `json:"branch,omitempty"`
}

// InteractiveRequest starts a session that a person drives in a terminal
// (tui-client) instead of the server driving it over stream-json. It is
// always spawned into a task.
type InteractiveRequest struct {
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id"`
	TicketID     string `json:"ticket_id"`
	AgentID      string `json:"agent_id,omitempty"`
	ZoneID       string `json:"zone_id,omitempty"`
	// Prompt is the optional first message; empty opens claude idle.
	Prompt     string `json:"prompt,omitempty"`
	Model      string `json:"model,omitempty"`
	AutoAccept string `json:"auto_accept,omitempty"` // "off" | "edits" | "all"
	BaseBranch string `json:"base_branch,omitempty"`
	// RunsOn is where the agent runs; empty means RunnerTUI. RunnerHost
	// names the client machine for a RunnerTUI session.
	RunsOn     domain.Runner `json:"runs_on,omitempty"`
	RunnerHost string        `json:"runner_host,omitempty"`
	// Size is the terminal size a RunnerServer session starts at.
	Size TermSize `json:"size,omitempty"`
	// ParentSessionID is the session starting this one on its own task (the
	// start_task_session tool); empty when a person starts it.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// Mode layers a role on the agent (see domain.SessionMode); empty is none.
	Mode string `json:"mode,omitempty"`
	// StatusCheckMinutes is the status-check loop the task's architect wants
	// on the session it starts: nil for the default (10 minutes), 0 for none,
	// otherwise 2–240. Ignored when the starter is not the architect.
	StatusCheckMinutes *int `json:"status_check_minutes,omitempty"`
}

// ResumeRequest reopens an ended interactive session, on RunsOn — which need
// not be where it ran before.
type ResumeRequest struct {
	SessionID  string        `json:"session_id"`
	RunsOn     domain.Runner `json:"runs_on,omitempty"`
	RunnerHost string        `json:"runner_host,omitempty"`
	Size       TermSize      `json:"size,omitempty"`
}

// SessionEvent is the unified wire + persistence event.
type SessionEvent struct {
	Seq       int64                `json:"seq"`
	SessionID string               `json:"session_id"`
	Type      string               `json:"type"` // user_message|output|output_delta|tool_use|tool_result|status|approval_needed|approval_resolved|approval_expired|auto_run|usage|done|error|artifact|task_message|review_request|status_check|task_status
	Status    domain.SessionStatus `json:"status,omitempty"`
	Text      string               `json:"text,omitempty"`
	ToolName  string               `json:"tool_name,omitempty"`
	ToolInput json.RawMessage      `json:"tool_input,omitempty"`
	DeltaKind string               `json:"delta_kind,omitempty"` // text|thinking|tool_input, for output_delta
	Index     int                  `json:"index,omitempty"`      // content-block index, for output_delta
	Approval  *ApprovalInfo        `json:"approval,omitempty"`
	Usage     *Usage               `json:"usage,omitempty"`
	CostUSD   float64              `json:"cost_usd,omitempty"`
	// AutoRun carries the new gate state on an "auto_run" event. A pointer, not
	// a bool: omitempty would erase the switched-off event, and a client folding
	// the log would then never see the gate close.
	AutoRun *bool `json:"auto_run,omitempty"`
	// Artifact is the artifact an "artifact" event announces, as published or
	// re-published (its Revision says which). Nil on every other type.
	Artifact *domain.Artifact `json:"artifact,omitempty"`
	// The architect channel's objects, on the event types of the same names
	// (task_message, review_request, status_check, task_status).
	TaskMessage   *domain.TaskMessage      `json:"task_message,omitempty"`
	ReviewRequest *domain.ReviewRequest    `json:"review_request,omitempty"`
	StatusCheck   *StatusCheckEvent        `json:"status_check,omitempty"`
	TaskStatus    *domain.TaskStatusChange `json:"task_status,omitempty"`
	At            time.Time                `json:"at"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ApprovalInfo struct {
	ReqID    string          `json:"req_id"`
	ToolName string          `json:"tool_name"`
	Input    json.RawMessage `json:"input,omitempty"`
	// Questions is set only for AskUserQuestion calls, where the decision the
	// user owes the agent is an answer rather than a permission. Empty for
	// every other tool, and the client falls back to allow/deny.
	Questions []Question `json:"questions,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Preview is optional mockup/snippet content the CLI renders beside the
	// options. Markdown in a monospace box, per the tool's own contract.
	Preview string `json:"preview,omitempty"`
}

type Question struct {
	// Header is a very short chip label ("Auth method", "Approach").
	Header      string           `json:"header,omitempty"`
	Question    string           `json:"question"`
	MultiSelect bool             `json:"multi_select,omitempty"`
	Options     []QuestionOption `json:"options"`
}
