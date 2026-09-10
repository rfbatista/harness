package session

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

func fixture() *backend.Fake {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool"}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "main-repo"}}
	f.Sessions = []*domain.Session{{ID: "s1", ProjectID: "p1", RepositoryID: "r1", Task: "Fix login", Status: domain.SessionIdle, Branch: "fix-login-1a2b", CostUSD: 0.42}}
	f.Emit("s1", orchestration.SessionEvent{Type: "user_message", Text: "Fix login", Status: domain.SessionThinking})
	f.Emit("s1", orchestration.SessionEvent{Type: "output", Text: "On it. Reading the router."})
	f.Emit("s1", orchestration.SessionEvent{Type: "status", Status: domain.SessionIdle})
	return f
}

func ctx() core.Context { return core.Context{Theme: theme.Dark(), Width: 100, Height: 30} }

func open(f *backend.Fake) Model {
	m := New(ctx(), f, backend.Load(f), "s1")
	return m
}

func ch(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func ctrl(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func typeKeys(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(ch(r))
	}
	return m
}

// deliver runs the pending wait command once (an event must already be
// emitted) and feeds the resulting message back.
func deliver(m Model, wait tea.Cmd) (Model, tea.Cmd) {
	msg := wait()
	return m.Update(msg)
}

// run executes a non-blocking command and feeds its results back, collecting
// messages addressed to the root.
func run(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case BackMsg, ClosedMsg, core.ToastMsg, core.RefreshMsg:
			out = append(out, msg)
		case nil:
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			// waitForEvent commands block; never re-run them here.
			if _, isEvent := msg.(EventMsg); !isEvent {
				queue = append(queue, next)
			}
		}
	}
	return m, out
}

func press(m Model, k tea.KeyPressMsg) (Model, []tea.Msg) {
	m, cmd := m.Update(k)
	return run(m, cmd)
}

func TestNewSeedsTimelineFromReplay(t *testing.T) {
	m := open(fixture())
	v := plain(m.View())
	for _, want := range []string{"Fix login", "On it. Reading the router.", "main-repo", "fix-login-1a2b", "$0.42"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	if !strings.Contains(v, "your turn") {
		t.Fatalf("idle should read as your turn:\n%s", v)
	}
}

func TestLiveEventsAppendAndDeltasStreamThenSettle(t *testing.T) {
	f := fixture()
	m := open(f)
	wait := m.Init()
	f.EmitDelta("s1", orchestration.SessionEvent{Type: "output_delta", DeltaKind: "text", Text: "Found "})
	m, wait = deliver(m, wait)
	f.EmitDelta("s1", orchestration.SessionEvent{Type: "output_delta", DeltaKind: "text", Text: "the bug"})
	m, wait = deliver(m, wait)
	if !strings.Contains(plain(m.View()), "Found the bug") {
		t.Fatalf("delta tail expected:\n%s", plain(m.View()))
	}
	f.Emit("s1", orchestration.SessionEvent{Type: "output", Text: "Found the bug in router.go"})
	m, _ = deliver(m, wait)
	v := plain(m.View())
	if strings.Count(v, "Found the bug") != 1 || !strings.Contains(v, "router.go") {
		t.Fatalf("output should replace the tail:\n%s", v)
	}
}

func TestComposerSendsWithCtrlS(t *testing.T) {
	f := fixture()
	m := open(f)
	m = typeKeys(m, "Also fix logout")
	m, _ = press(m, ctrl('s'))
	if len(f.Sent) != 1 || f.Sent[0].Text != "Also fix logout" {
		t.Fatalf("send: %+v", f.Sent)
	}
	if strings.Contains(plain(m.View()), "Also fix logout") {
		t.Fatalf("composer should clear after a successful send:\n%s", plain(m.View()))
	}
}

func TestComposerSendFailureKeepsTextAndFeed(t *testing.T) {
	f := fixture()
	m := open(f)
	f.Fail = &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not running"}
	m = typeKeys(m, "hello")
	m, _ = press(m, ctrl('s'))
	v := plain(m.View())
	if !strings.Contains(v, "session not running") || !strings.Contains(v, "hello") || !strings.Contains(v, "On it.") {
		t.Fatalf("failure should be inline, text and feed kept:\n%s", v)
	}
}

func TestApprovalBarApprovesAndDenies(t *testing.T) {
	f := fixture()
	f.Sessions[0].Status = domain.SessionWaitingApproval
	f.Emit("s1", orchestration.SessionEvent{Type: "approval_needed", Status: domain.SessionWaitingApproval, ToolName: "Bash",
		Approval: &orchestration.ApprovalInfo{ReqID: "r1", ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`)}})
	m := open(f)
	v := plain(m.View())
	if !strings.Contains(v, "Bash") || !strings.Contains(v, "rm -rf build") || !strings.Contains(v, "y approve") {
		t.Fatalf("approval bar:\n%s", v)
	}
	m, _ = press(m, ch('y'))
	if len(f.Decisions) != 1 || !f.Decisions[0].Allow || f.Decisions[0].ReqID != "r1" {
		t.Fatalf("approve: %+v", f.Decisions)
	}
	f.Emit("s1", orchestration.SessionEvent{Type: "approval_needed", Status: domain.SessionWaitingApproval, ToolName: "Write",
		Approval: &orchestration.ApprovalInfo{ReqID: "r2", ToolName: "Write", Input: json.RawMessage(`{"file_path":"/etc/hosts"}`)}})
	m = open(f)
	m, _ = press(m, ch('n'))
	if len(f.Decisions) != 2 || f.Decisions[1].Allow {
		t.Fatalf("deny: %+v", f.Decisions)
	}
}

func TestQuestionPanelAnswersAllQuestions(t *testing.T) {
	f := fixture()
	f.Sessions[0].Status = domain.SessionWaitingApproval
	f.Emit("s1", orchestration.SessionEvent{Type: "approval_needed", Status: domain.SessionWaitingApproval, ToolName: orchestration.AskUserQuestionTool,
		Approval: &orchestration.ApprovalInfo{ReqID: "q1", ToolName: orchestration.AskUserQuestionTool, Questions: []orchestration.Question{
			{Header: "DB", Question: "Which database?", Options: []orchestration.QuestionOption{{Label: "sqlite"}, {Label: "postgres"}}},
			{Header: "Tests", Question: "Which tests?", MultiSelect: true, Options: []orchestration.QuestionOption{{Label: "unit"}, {Label: "e2e"}}},
		}}})
	m := open(f)
	v := plain(m.View())
	if !strings.Contains(v, "Which database?") || !strings.Contains(v, "postgres") {
		t.Fatalf("question panel:\n%s", v)
	}
	m, _ = press(m, ctrl('s')) // nothing answered yet
	if len(f.Decisions) != 0 {
		t.Fatal("submit must wait until every question is answered")
	}
	m, _ = press(m, ch('j'))                           // postgres
	m, _ = press(m, ch(' '))                           // pick
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab}) // next question
	m, _ = press(m, ch(' '))                           // unit
	m, _ = press(m, ch('j'))
	m, _ = press(m, ch(' ')) // e2e
	m, _ = press(m, ctrl('s'))
	if len(f.Decisions) != 1 {
		t.Fatalf("answer: %+v\n%s", f.Decisions, plain(m.View()))
	}
	d := f.Decisions[0]
	if d.Answers["Which database?"] != "postgres" || d.Answers["Which tests?"] != "unit, e2e" || d.ReqID != "q1" {
		t.Fatalf("answers: %+v", d.Answers)
	}
}

func TestQuestionEscSkipsAsDeny(t *testing.T) {
	f := fixture()
	f.Emit("s1", orchestration.SessionEvent{Type: "approval_needed", Status: domain.SessionWaitingApproval, ToolName: orchestration.AskUserQuestionTool,
		Approval: &orchestration.ApprovalInfo{ReqID: "q1", ToolName: orchestration.AskUserQuestionTool, Questions: []orchestration.Question{{Question: "Q?", Options: []orchestration.QuestionOption{{Label: "a"}}}}}})
	m := open(f)
	_, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(f.Decisions) != 1 || f.Decisions[0].Allow {
		t.Fatalf("esc should deny: %+v", f.Decisions)
	}
}

func TestEndedModeWhenTerminal(t *testing.T) {
	f := fixture()
	f.Sessions[0].Status = domain.SessionDone
	f.Emit("s1", orchestration.SessionEvent{Type: "done", Status: domain.SessionDone})
	m := open(f)
	v := plain(m.View())
	if !strings.Contains(v, "Session done") || strings.Contains(v, "ctrl+s send") {
		t.Fatalf("ended bar expected, no composer:\n%s", v)
	}
}

func TestAutoRunStopAndDelete(t *testing.T) {
	f := fixture()
	m := open(f)
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab}) // feed focus so letters are commands
	m, _ = press(m, ch('a'))
	if !f.Sessions[0].AutoRun {
		t.Fatal("a should enable auto-run")
	}
	m, _ = press(m, ch('S'))
	if f.Sessions[0].Status != domain.SessionStopped {
		t.Fatal("S should stop")
	}
	m, _ = press(m, ch('D'))
	m, out := press(m, ch('y'))
	if len(f.Sessions) != 0 {
		t.Fatal("D then y should delete")
	}
	closed := false
	for _, o := range out {
		if _, ok := o.(ClosedMsg); ok {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("ClosedMsg expected after delete, got %v", out)
	}
}

func TestEscInFeedFocusGoesBackAndKeepsSubscription(t *testing.T) {
	f := fixture()
	m := open(f)
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	_, out := press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(out) != 1 {
		t.Fatalf("BackMsg expected, got %v", out)
	}
	if _, ok := out[0].(BackMsg); !ok {
		t.Fatalf("want BackMsg got %#v", out[0])
	}
	if !m.Subscribed() {
		t.Fatal("going back must keep the subscription alive")
	}
	m.Close()
	if m.Subscribed() {
		t.Fatal("Close should cancel the subscription")
	}
}

func TestToolCallsCollapseAndExpand(t *testing.T) {
	f := fixture()
	f.Emit("s1", orchestration.SessionEvent{Type: "tool_use", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./...","timeout":600}`)})
	f.Emit("s1", orchestration.SessionEvent{Type: "tool_result", Text: "ok  operators-mcp 0.4s"})
	m := open(f)
	v := plain(m.View())
	if !strings.Contains(v, "Bash") || !strings.Contains(v, "go test ./...") || strings.Contains(v, "timeout") {
		t.Fatalf("collapsed tool call shows summary only:\n%s", v)
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab}) // feed focus
	m, _ = press(m, ch('G'))                           // last item
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	v = plain(m.View())
	if !strings.Contains(v, "timeout") || !strings.Contains(v, "operators-mcp 0.4s") {
		t.Fatalf("expanded tool call shows input and result:\n%s", v)
	}
}
