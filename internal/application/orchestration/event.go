package orchestration

import (
	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The session event types are the orchestration's wire contract; they live in
// ports so inbound adapters can name them without importing this package.
type (
	SessionEvent = ports.SessionEvent
	Usage        = ports.Usage
	ApprovalInfo = ports.ApprovalInfo
)

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
