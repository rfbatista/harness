package session

import (
	"encoding/json"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

func ev(seq int64, typ string) orchestration.SessionEvent {
	return orchestration.SessionEvent{Seq: seq, SessionID: "s1", Type: typ, At: time.Unix(seq, 0)}
}

func withStatus(e orchestration.SessionEvent, st domain.SessionStatus) orchestration.SessionEvent {
	e.Status = st
	return e
}

func withText(e orchestration.SessionEvent, t string) orchestration.SessionEvent {
	e.Text = t
	return e
}

func TestFoldBuildsMessagesAndDividers(t *testing.T) {
	tl := Fold([]orchestration.SessionEvent{
		withStatus(withText(ev(1, "user_message"), "fix it"), domain.SessionThinking),
		withText(ev(2, "output"), "  done  "),
		withStatus(ev(3, "status"), domain.SessionIdle),
	})
	if len(tl.Items) != 4 {
		t.Fatalf("want divider, user, assistant, divider; got %+v", tl.Items)
	}
	if tl.Items[0].Kind != ItemStatus || tl.Items[1].Kind != ItemUser || tl.Items[1].Text != "fix it" {
		t.Fatalf("head: %+v", tl.Items[:2])
	}
	if tl.Items[2].Kind != ItemAssistant || tl.Items[2].Text != "done" || tl.Items[3].Kind != ItemStatus {
		t.Fatalf("tail: %+v", tl.Items[2:])
	}
	if tl.Status != domain.SessionIdle {
		t.Fatalf("status: %v", tl.Status)
	}
}

func TestFoldPairsToolResultsInOrder(t *testing.T) {
	a := ev(1, "tool_use")
	a.ToolName = "Read"
	a.ToolInput = json.RawMessage(`{"file_path":"/x/main.go"}`)
	b := ev(2, "tool_use")
	b.ToolName = "Bash"
	b.ToolInput = json.RawMessage(`{"command":"go test ./..."}`)
	tl := Fold([]orchestration.SessionEvent{a, b, withText(ev(3, "tool_result"), `"ok"`), withText(ev(4, "tool_result"), "PASS")})
	if len(tl.Items) != 2 {
		t.Fatalf("results should fold into the calls: %+v", tl.Items)
	}
	if tl.Items[0].ToolName != "Read" || tl.Items[0].Result != "ok" || tl.Items[0].Summary != "/x/main.go" {
		t.Fatalf("first call: %+v", tl.Items[0])
	}
	if tl.Items[1].ToolName != "Bash" || tl.Items[1].Result != "PASS" || tl.Items[1].Summary != "go test ./..." {
		t.Fatalf("second call: %+v", tl.Items[1])
	}
}

func TestFoldTracksPendingApprovalsAndQuestions(t *testing.T) {
	need := withStatus(ev(1, "approval_needed"), domain.SessionWaitingApproval)
	need.ToolName = "Bash"
	need.Approval = &orchestration.ApprovalInfo{ReqID: "r1", ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`)}
	ask := withStatus(ev(2, "approval_needed"), domain.SessionWaitingApproval)
	ask.ToolName = orchestration.AskUserQuestionTool
	ask.Approval = &orchestration.ApprovalInfo{ReqID: "r2", ToolName: orchestration.AskUserQuestionTool, Questions: []orchestration.Question{{Question: "Which DB?", Options: []orchestration.QuestionOption{{Label: "sqlite"}, {Label: "postgres"}}}}}
	tl := Fold([]orchestration.SessionEvent{need, ask})
	if len(tl.Pending) != 2 || tl.Pending[0].ReqID != "r1" || !tl.Pending[1].IsQuestion() {
		t.Fatalf("pending: %+v", tl.Pending)
	}
	resolved := withStatus(withText(ev(3, "approval_resolved"), "allowed"), domain.SessionThinking)
	resolved.Approval = &orchestration.ApprovalInfo{ReqID: "r1"}
	expired := withStatus(withText(ev(4, "approval_expired"), "timed out"), domain.SessionThinking)
	expired.Approval = &orchestration.ApprovalInfo{ReqID: "r2"}
	tl = Fold([]orchestration.SessionEvent{need, ask, resolved, expired})
	if len(tl.Pending) != 0 {
		t.Fatalf("both should be retired: %+v", tl.Pending)
	}
	var kinds []ItemKind
	for _, it := range tl.Items {
		kinds = append(kinds, it.Kind)
	}
	want := []ItemKind{ItemStatus, ItemApproval, ItemApproval, ItemStatus, ItemResolved, ItemResolved}
	if len(kinds) != len(want) {
		t.Fatalf("kinds: %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds: %v want %v", kinds, want)
		}
	}
	if !tl.Items[5].Expired || tl.Items[4].Expired {
		t.Fatalf("expiry flag: %+v %+v", tl.Items[4], tl.Items[5])
	}
}

func TestFoldTerminalEventClearsPendingAndEnds(t *testing.T) {
	need := withStatus(ev(1, "approval_needed"), domain.SessionWaitingApproval)
	need.Approval = &orchestration.ApprovalInfo{ReqID: "r1", ToolName: "Bash"}
	tl := Fold([]orchestration.SessionEvent{need, withStatus(ev(2, "done"), domain.SessionDone)})
	if len(tl.Pending) != 0 || tl.Items[len(tl.Items)-1].Kind != ItemEnded || tl.Status != domain.SessionDone {
		t.Fatalf("done should retire approvals and end: %+v", tl)
	}
	tl = Fold([]orchestration.SessionEvent{need, withStatus(withText(ev(2, "error"), "boom"), domain.SessionFailed)})
	if len(tl.Pending) != 0 || tl.Items[len(tl.Items)-1].Kind != ItemError {
		t.Fatalf("failed should retire approvals: %+v", tl)
	}
	// A transient error carries no status and keeps the request alive.
	tl = Fold([]orchestration.SessionEvent{need, withText(ev(2, "error"), "hiccup")})
	if len(tl.Pending) != 1 {
		t.Fatalf("transient error must not retire approvals: %+v", tl.Pending)
	}
}

func TestFoldUsageAndAutoRun(t *testing.T) {
	on := true
	u := ev(1, "usage")
	u.CostUSD = 0.5
	u.Usage = &orchestration.Usage{InputTokens: 100, OutputTokens: 20}
	u.Text = "final answer"
	out := withText(ev(0, "output"), "final answer")
	out.Seq = 0 // sorted first
	ar := ev(2, "auto_run")
	ar.AutoRun = &on
	tl := Fold([]orchestration.SessionEvent{u, out, ar})
	if tl.Usage.CostUSD != 0.5 || tl.Usage.InputTokens != 100 || tl.Usage.OutputTokens != 20 || !tl.AutoRun {
		t.Fatalf("usage/auto-run: %+v", tl)
	}
	assistants := 0
	for _, it := range tl.Items {
		if it.Kind == ItemAssistant {
			assistants++
		}
	}
	if assistants != 1 {
		t.Fatalf("usage text equal to the last message must not duplicate it: %+v", tl.Items)
	}
}

func TestFoldIgnoresDeltasAndSortsBySeq(t *testing.T) {
	tl := Fold([]orchestration.SessionEvent{
		withText(ev(2, "output"), "second"),
		withText(ev(1, "output"), "first"),
		{Seq: 0, Type: "output_delta", Text: "par", DeltaKind: "text"},
	})
	if len(tl.Items) != 2 || tl.Items[0].Text != "first" || tl.Items[1].Text != "second" {
		t.Fatalf("order: %+v", tl.Items)
	}
}

func TestDeltasAccumulateByIndexAndKindThenClearOnOutput(t *testing.T) {
	var d Deltas
	d.Apply(orchestration.SessionEvent{Type: "output_delta", DeltaKind: "text", Index: 0, Text: "Hel"})
	d.Apply(orchestration.SessionEvent{Type: "output_delta", DeltaKind: "text", Index: 0, Text: "lo"})
	d.Apply(orchestration.SessionEvent{Type: "output_delta", DeltaKind: "thinking", Index: 1, Text: "hmm"})
	if d.Text() != "Hello" || d.Thinking() != "hmm" {
		t.Fatalf("accumulate: %q %q", d.Text(), d.Thinking())
	}
	d.Apply(orchestration.SessionEvent{Type: "output", Text: "Hello"})
	if !d.Empty() {
		t.Fatal("a durable output should clear the live tail")
	}
}
