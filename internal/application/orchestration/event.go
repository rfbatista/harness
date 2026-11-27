package orchestration

import (
	"encoding/json"
	"strings"

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
		ev.CallUsage = fromCallUsage(ce.CallUsage)
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
		ev.CallUsage = fromCallUsage(ce.CallUsage)
		return ev, domain.SessionThinking, true
	case llmkit.EventToolResult:
		ev.Type = "tool_result"
		ev.Text = ce.Text
		ev.IsError = toolResultFailed(ce.Raw, ce.Text)
		return ev, "", false
	case llmkit.EventHook:
		ev.Type = "status"
		return ev, "", false
	case llmkit.EventResult:
		ev.Type = "usage"
		ev.Text = ce.Text
		ev.CostUSD = ce.CostUSD
		ev.ContextWindows = ce.ContextWindows
		if ce.Usage != nil {
			ev.Usage = &Usage{InputTokens: ce.Usage.InputTokens, OutputTokens: ce.Usage.OutputTokens,
				CacheCreationInputTokens: ce.Usage.CacheCreationInputTokens, CacheReadInputTokens: ce.Usage.CacheReadInputTokens}
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

// fromCallUsage is one API call's usage as the session event carries it.
func fromCallUsage(u *llmkit.Usage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens}
}

// toolResultFailed reports whether the CLI marked the tool result with this
// text as an error. A line can carry several results, each its own event with
// the same raw line, so the block is found by its text; a lone block needs no
// match. llmkit's event has no flag of its own, and Raw is the fidelity it
// keeps for this.
func toolResultFailed(raw json.RawMessage, text string) bool {
	var line struct {
		Message struct {
			Content []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
				IsError bool            `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &line) != nil {
		return false
	}
	var results []bool
	matched := false
	failed := false
	for _, c := range line.Message.Content {
		if c.Type != "tool_result" {
			continue
		}
		results = append(results, c.IsError)
		if resultText(c.Content) == text {
			// Results with the same text on one line cannot be told apart: it
			// is a failure only when every one of them is.
			failed = c.IsError && (!matched || failed)
			matched = true
		}
	}
	if matched {
		return failed
	}
	return len(results) == 1 && results[0]
}

// resultText is the text of a tool_result block's content: a JSON string or an
// array of text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
}
