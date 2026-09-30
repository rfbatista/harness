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

// InteractiveSessions provisions and records sessions whose CLI runs in the
// client's terminal (tui-client); the client runs the returned Launch.
type InteractiveSessions interface {
	StartInteractive(ctx context.Context, req InteractiveRequest) (*domain.Session, Launch, error)
	ResumeInteractive(ctx context.Context, id string) (*domain.Session, Launch, error)
	EndInteractive(ctx context.Context, id string, exitCode int, closedByUser bool) (*domain.Session, error)
	RecordClaudeSession(ctx context.Context, id, claudeSessionID string) error
}

// Orchestration is the whole session surface, for an adapter that serves all
// of it (the HTTP API).
type Orchestration interface {
	SessionLauncher
	SessionControl
	SessionReader
	SessionStream
	InteractiveSessions
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
}

// Launch is how the client runs an interactive session's CLI: the claude
// binary with Args, in Dir, with Env added to its own environment.
type Launch struct {
	SessionID string   `json:"session_id"`
	Dir       string   `json:"dir"`
	Args      []string `json:"args"`
	Env       []string `json:"env,omitempty"`
}

// SessionEvent is the unified wire + persistence event.
type SessionEvent struct {
	Seq       int64                `json:"seq"`
	SessionID string               `json:"session_id"`
	Type      string               `json:"type"` // user_message|output|output_delta|tool_use|tool_result|status|approval_needed|approval_resolved|approval_expired|auto_run|usage|done|error
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
	AutoRun *bool     `json:"auto_run,omitempty"`
	At      time.Time `json:"at"`
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
