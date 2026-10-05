package orchestration

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

func TestFromAgentEvent(t *testing.T) {
	ev, status, has := fromAgentEvent(llmkit.Event{Type: llmkit.EventAssistant, Text: "hi"})
	if ev.Type != "output" || ev.Text != "hi" {
		t.Fatalf("assistant map wrong: %+v", ev)
	}
	if !has || status != domain.SessionThinking {
		t.Fatalf("assistant status wrong: %v %v", status, has)
	}

	// The turn boundaries are resolved by Service.pump, which knows whether a
	// turn is in flight; the pure mapping must not guess at them.
	_, _, has = fromAgentEvent(llmkit.Event{Type: llmkit.EventSystem, Subtype: "init"})
	if has {
		t.Fatal("system/init must not carry a status of its own")
	}
	_, _, has = fromAgentEvent(llmkit.Event{Type: llmkit.EventResult})
	if has {
		t.Fatal("result must not carry a status of its own")
	}

	ev, _, _ = fromAgentEvent(llmkit.Event{Type: llmkit.EventToolUse, ToolName: "Bash"})
	if ev.Type != "tool_use" || ev.ToolName != "Bash" {
		t.Fatalf("tool_use map wrong: %+v", ev)
	}

	ev, _, _ = fromAgentEvent(llmkit.Event{Type: llmkit.EventResult, Text: "done", CostUSD: 0.4, Usage: &llmkit.Usage{InputTokens: 3, OutputTokens: 7}})
	if ev.Type != "usage" || ev.Usage == nil || ev.Usage.OutputTokens != 7 || ev.CostUSD != 0.4 {
		t.Fatalf("result map wrong: %+v", ev)
	}

	ev, _, has = fromAgentEvent(llmkit.Event{Type: llmkit.EventDelta, DeltaKind: llmkit.DeltaText, Text: "Hi", Index: 0})
	if ev.Type != "output_delta" || ev.Text != "Hi" || ev.DeltaKind != "text" {
		t.Fatalf("delta map wrong: %+v", ev)
	}
	if has {
		t.Fatal("delta must not carry a status transition")
	}
	ev, _, _ = fromAgentEvent(llmkit.Event{Type: llmkit.EventDelta, DeltaKind: llmkit.DeltaToolInput, ToolName: "Bash", Index: 1})
	if ev.Type != "output_delta" || ev.DeltaKind != "tool_input" || ev.ToolName != "Bash" || ev.Index != 1 {
		t.Fatalf("tool_input delta map wrong: %+v", ev)
	}

	_, status, has = fromAgentEvent(llmkit.Event{Type: llmkit.EventExit, Subtype: "done"})
	if !has || status != domain.SessionDone {
		t.Fatalf("exit done wrong: %v %v", status, has)
	}
	_, status, _ = fromAgentEvent(llmkit.Event{Type: llmkit.EventExit, Subtype: "failed", Err: "boom"})
	if status != domain.SessionFailed {
		t.Fatalf("exit failed wrong: %v", status)
	}
}

func TestLogSessionEvent(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	// Capture at Debug so delta lines are emitted too.
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	logSessionEvent("s1", SessionEvent{Type: "output", Text: "hello there"})
	logSessionEvent("s1", SessionEvent{Type: "tool_use", ToolName: "Bash"})
	logSessionEvent("s1", SessionEvent{Type: "output_delta", DeltaKind: "text", Text: "Hi"})
	logSessionEvent("s1", SessionEvent{Type: "error", Text: "boom"})

	out := buf.String()
	for _, want := range []string{
		`level=INFO msg="claude message" session=s1 type=output`,
		`type=tool_use tool=Bash`,
		`level=DEBUG msg="claude delta" session=s1 kind=text`,
		`level=ERROR msg="claude message" session=s1 type=error`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in log output:\n%s", want, out)
		}
	}
}

func TestSessionEvent_ArtifactIsOptional(t *testing.T) {
	plain, _ := json.Marshal(SessionEvent{Type: "status"})
	if strings.Contains(string(plain), "artifact") {
		t.Fatalf("artifact must be omitted when nil: %s", plain)
	}
	with, _ := json.Marshal(SessionEvent{Type: "artifact", Text: "first cut", Artifact: &domain.Artifact{ID: "a1", Kind: domain.ArtifactPage, Path: "design/card.html", Revision: 1}})
	if !strings.Contains(string(with), `"artifact":{"id":"a1"`) || !strings.Contains(string(with), `"type":"artifact"`) {
		t.Fatalf("artifact event json = %s", with)
	}
}
