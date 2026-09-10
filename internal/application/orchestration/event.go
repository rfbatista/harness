package orchestration

import (
	"encoding/json"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

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

// fromAgentEvent maps a low-level llmkit.Event to a SessionEvent and the
// status it implies (hasStatus=false means leave status unchanged).
//
// The two turn-boundary events — "system"/init and "result" — deliberately
// carry no status here: whether init means "ready" and whether a result ends
// the turn depend on whether a turn is currently in flight, which is
// per-session state this pure mapping has no access to. Service.pump applies
// them.
func fromAgentEvent(ce llmkit.Event) (ev SessionEvent, status domain.SessionStatus, hasStatus bool) {
	ev = SessionEvent{SessionID: ce.SessionID, At: ce.At}
	switch ce.Type {
	case llmkit.EventSystem:
		ev.Type = "status"
		return ev, "", false
	case llmkit.EventAssistant:
		ev.Type = "output"
		ev.Text = ce.Text
		return ev, domain.SessionThinking, true
	case llmkit.EventDelta:
		// Ephemeral live-streaming fragment; routed past persistence/status.
		ev.Type = "output_delta"
		ev.Text = ce.Text
		ev.ToolName = ce.ToolName
		ev.DeltaKind = string(ce.DeltaKind)
		ev.Index = ce.Index
		return ev, "", false
	case llmkit.EventToolUse:
		ev.Type = "tool_use"
		ev.ToolName = ce.ToolName
		ev.ToolInput = ce.ToolInput
		return ev, domain.SessionThinking, true
	case llmkit.EventToolResult:
		ev.Type = "tool_result"
		ev.Text = ce.Text
		return ev, "", false
	case llmkit.EventHook:
		ev.Type = "status"
		return ev, "", false
	case llmkit.EventResult:
		ev.Type = "usage"
		ev.Text = ce.Text
		ev.CostUSD = ce.CostUSD
		if ce.Usage != nil {
			ev.Usage = &Usage{InputTokens: ce.Usage.InputTokens, OutputTokens: ce.Usage.OutputTokens}
		}
		return ev, "", false
	case llmkit.EventExit:
		if ce.Subtype == "failed" {
			ev.Type = "error"
			ev.Text = ce.Err
			return ev, domain.SessionFailed, true
		}
		if ce.Subtype == "stopped" {
			ev.Type = "done"
			return ev, domain.SessionStopped, true
		}
		ev.Type = "done"
		return ev, domain.SessionDone, true
	case llmkit.EventError:
		ev.Type = "error"
		ev.Text = ce.Err
		return ev, "", false
	default:
		ev.Type = "status"
		return ev, "", false
	}
}
