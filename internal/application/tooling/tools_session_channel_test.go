package tooling

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeChannel stands in for the server's architect channel: it records what
// each call was given, answers from a role map, and fails every call with err
// when set, so the tools' pass-through can be checked.
type fakeChannel struct {
	roles     map[string]domain.SessionRole
	architect string
	err       error
	delivered bool
	checks    []*domain.StatusCheck

	calls   []string
	caller  string // the session id the port was told is acting
	target  string // the other session or review a call names
	message ports.TaskMessageInput
	filter  ports.TaskMessageFilter
	review  ports.ReviewRequestInput
	state   domain.ReviewState
	status  domain.TicketStatus
	reason  string
	every   int
}

func (f *fakeChannel) record(name string) error {
	f.calls = append(f.calls, name)
	return f.err
}

func (f *fakeChannel) called(name string) bool { return slices.Contains(f.calls, name) }

func (f *fakeChannel) TaskArchitect(_ context.Context, taskID string) (*domain.Session, error) {
	if err := f.record("TaskArchitect"); err != nil {
		return nil, err
	}
	if f.architect == "" {
		return nil, nil
	}
	return &domain.Session{ID: f.architect, TicketID: taskID}, nil
}

func (f *fakeChannel) SessionRole(_ context.Context, sessionID string) (domain.SessionRole, error) {
	if err := f.record("SessionRole"); err != nil {
		return "", err
	}
	return f.roles[sessionID], nil
}

func (f *fakeChannel) storeMessage(name, caller, target string, msg ports.TaskMessageInput) (*domain.TaskMessage, error) {
	if err := f.record(name); err != nil {
		return nil, err
	}
	f.caller, f.target, f.message = caller, target, msg
	return &domain.TaskMessage{ID: "m1", FromSessionID: caller, ToSessionID: target, Kind: msg.Kind, Body: msg.Body, Delivered: f.delivered}, nil
}

func (f *fakeChannel) SendToArchitect(_ context.Context, from string, msg ports.TaskMessageInput) (*domain.TaskMessage, error) {
	return f.storeMessage("SendToArchitect", from, f.architect, msg)
}

func (f *fakeChannel) ReplyFromArchitect(_ context.Context, architectID, to string, msg ports.TaskMessageInput) (*domain.TaskMessage, error) {
	return f.storeMessage("ReplyFromArchitect", architectID, to, msg)
}

func (f *fakeChannel) ListTaskMessages(context.Context, string, ports.TaskMessageFilter) ([]*domain.TaskMessage, error) {
	return nil, f.record("ListTaskMessages")
}

func (f *fakeChannel) ListSessionMessages(_ context.Context, viewer string, fl ports.TaskMessageFilter) ([]*domain.TaskMessage, error) {
	if err := f.record("ListSessionMessages"); err != nil {
		return nil, err
	}
	f.caller, f.filter = viewer, fl
	return []*domain.TaskMessage{{ID: "m1", Kind: domain.MessageQuestion, Body: "which port?"}}, nil
}

func (f *fakeChannel) RequestUserReview(_ context.Context, architectID string, req ports.ReviewRequestInput) (*domain.ReviewRequest, error) {
	if err := f.record("RequestUserReview"); err != nil {
		return nil, err
	}
	f.caller, f.review = architectID, req
	return &domain.ReviewRequest{ID: "r1", Subject: req.Subject, State: domain.ReviewPending}, nil
}

func (f *fakeChannel) WithdrawReview(_ context.Context, architectID, reviewID string) (*domain.ReviewRequest, error) {
	if err := f.record("WithdrawReview"); err != nil {
		return nil, err
	}
	f.caller, f.target = architectID, reviewID
	return &domain.ReviewRequest{ID: reviewID, State: domain.ReviewWithdrawn}, nil
}

func (f *fakeChannel) ListReviewRequests(_ context.Context, _ string, state domain.ReviewState) ([]*domain.ReviewRequest, error) {
	if err := f.record("ListReviewRequests"); err != nil {
		return nil, err
	}
	f.state = state
	return []*domain.ReviewRequest{{ID: "r1", State: domain.ReviewPending}}, nil
}

func (f *fakeChannel) RespondReview(context.Context, string, domain.ReviewState, string) (*domain.ReviewRequest, bool, error) {
	return nil, false, f.record("RespondReview")
}

func (f *fakeChannel) SetTaskStatus(_ context.Context, by string, status domain.TicketStatus, reason string) (*domain.Ticket, error) {
	if err := f.record("SetTaskStatus"); err != nil {
		return nil, err
	}
	f.caller, f.status, f.reason = by, status, reason
	return &domain.Ticket{ID: "tk", Status: status}, nil
}

func (f *fakeChannel) SetStatusCheck(_ context.Context, architectID, delegateID string, every int) (*domain.StatusCheck, error) {
	if err := f.record("SetStatusCheck"); err != nil {
		return nil, err
	}
	f.caller, f.target, f.every = architectID, delegateID, every
	return &domain.StatusCheck{ArchitectSessionID: architectID, DelegateSessionID: delegateID, EveryMinutes: every, State: domain.StatusCheckActive}, nil
}

func (f *fakeChannel) ListStatusChecks(context.Context, string) ([]*domain.StatusCheck, error) {
	if err := f.record("ListStatusChecks"); err != nil {
		return nil, err
	}
	return f.checks, nil
}

var _ ports.TaskChannel = (*fakeChannel)(nil)

// channelFixture: one task with an architect ("arch"), its delegate ("dlg")
// and a peer a person started ("peer"), served by the tools over ch.
type channelFixture struct {
	ch      *fakeChannel
	tools   map[string]domain.Tool
	starter *fakeStarter
	taskID  string
}

func newChannelFixture(t *testing.T) *channelFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	task, _ := plan.CreateTicket(context.Background(), proj.ID, "Architect highlights", "", domain.TicketStatusInProgress)
	for _, s := range []*domain.Session{
		{ID: "arch", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api", Status: domain.SessionRunning, Mode: domain.SessionModeArchitect},
		{ID: "dlg", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api", Status: domain.SessionRunning, ParentSessionID: "arch"},
		{ID: "peer", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api", Status: domain.SessionRunning},
	} {
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	ch := &fakeChannel{
		roles:     map[string]domain.SessionRole{"arch": domain.RoleArchitect, "dlg": domain.RoleDelegate},
		architect: "arch",
		delivered: true,
	}
	starter := &fakeStarter{sessions: sessions}
	tools := map[string]domain.Tool{}
	for _, tl := range SessionTaskTools(plan, sessions, nil, nil,
		PeerStarter{Sessions: starter, Repositories: repoList{{ID: "r-api", Name: "api"}}}, ArtifactTooling{}, ch) {
		tools[tl.Name] = tl
	}
	return &channelFixture{ch: ch, tools: tools, starter: starter, taskID: task.ID}
}

// call runs a tool as sessionID and reads the result as the agent does: JSON.
func (f *channelFixture) call(t *testing.T, sessionID, tool string, args map[string]any) (map[string]any, error) {
	t.Helper()
	tl, ok := f.tools[tool]
	if !ok {
		t.Fatalf("tool %s not built", tool)
	}
	out, err := tl.Handler(WithSessionID(context.Background(), sessionID), args)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func (f *channelFixture) mustCall(t *testing.T, sessionID, tool string, args map[string]any) map[string]any {
	t.Helper()
	out, err := f.call(t, sessionID, tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return out
}

var channelToolNames = []string{
	"message_architect", "reply_to_session", "list_task_messages", "request_user_review",
	"withdraw_user_review", "list_review_requests", "set_status_check", "list_status_checks",
}

func TestChannelTools_AreListedForTheAllowList(t *testing.T) {
	for _, name := range channelToolNames {
		if !slices.Contains(SessionTaskToolNames, name) {
			t.Errorf("%s missing from SessionTaskToolNames", name)
		}
	}
}

func TestChannelTools_UnavailableWithoutAChannel(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, name := range channelToolNames {
		_, err := f.call(t, f.sessionID, name, map[string]any{"kind": "question", "body": "x", "session_id": "s", "review_id": "r", "every_minutes": 5, "subject": "s"})
		if errs.Code(err) != "UNAVAILABLE" {
			t.Errorf("%s without a channel: got %v, want UNAVAILABLE", name, err)
		}
	}
}

func TestMessageArchitect_SendsFromTheCaller(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "dlg", "message_architect", map[string]any{
		"kind": "status_report", "body": "plan is half done", "subject": "plan", "status": "working",
		"document_ids": []any{"d1"}, "artifact_ids": []any{"a1"}, "in_reply_to": "m0",
		"session_id": "arch", // not an input: the sender is always the caller
	})
	want := ports.TaskMessageInput{
		Kind: domain.MessageStatusReport, Subject: "plan", Body: "plan is half done", Status: domain.ReportWorking,
		InReplyTo: "m0", DocumentIDs: []string{"d1"}, ArtifactIDs: []string{"a1"},
	}
	if f.ch.caller != "dlg" || !equalMessage(f.ch.message, want) {
		t.Fatalf("sent %+v from %q, want %+v from dlg", f.ch.message, f.ch.caller, want)
	}
	if out["delivered"] != true || out["message"].(map[string]any)["id"] != "m1" {
		t.Fatalf("out = %+v", out)
	}
	if _, ok := out["note"]; ok {
		t.Fatalf("a delivered message needs no note: %+v", out)
	}
}

func TestMessageArchitect_NotesAQueuedMessage(t *testing.T) {
	f := newChannelFixture(t)
	f.ch.delivered = false
	out := f.mustCall(t, "dlg", "message_architect", map[string]any{"kind": "question", "body": "which port?"})
	note, _ := out["note"].(string)
	if out["delivered"] != false || !strings.Contains(note, "turn ends") || !strings.Contains(note, "list_task_messages") {
		t.Fatalf("out = %+v", out)
	}
}

func TestMessageArchitect_RefusesAnEmptyBodyWithoutCallingThePort(t *testing.T) {
	f := newChannelFixture(t)
	_, err := f.call(t, "dlg", "message_architect", map[string]any{"kind": "question", "body": "  "})
	wantCode(t, err, "INVALID_INPUT")
	if len(f.ch.calls) != 0 {
		t.Fatalf("port called: %v", f.ch.calls)
	}
}

func TestMessageArchitect_PassesThePortsErrorThrough(t *testing.T) {
	f := newChannelFixture(t)
	f.ch.err = &domain.StructuredError{Code: "NO_ARCHITECT", Message: "the task has no architect"}
	_, err := f.call(t, "peer", "message_architect", map[string]any{"kind": "question", "body": "anyone?"})
	wantCode(t, err, "NO_ARCHITECT")
}

func TestReplyToSession_RepliesAsTheCaller(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "arch", "reply_to_session", map[string]any{
		"session_id": "dlg", "body": "split the port", "in_reply_to": "m1", "verdict": "changes_requested",
	})
	want := ports.TaskMessageInput{Kind: domain.MessageReply, Body: "split the port", InReplyTo: "m1", Verdict: domain.VerdictChangesRequested}
	if f.ch.caller != "arch" || f.ch.target != "dlg" || !equalMessage(f.ch.message, want) {
		t.Fatalf("replied %+v from %q to %q", f.ch.message, f.ch.caller, f.ch.target)
	}
	if out["delivered"] != true {
		t.Fatalf("out = %+v", out)
	}

	_, err := f.call(t, "arch", "reply_to_session", map[string]any{"body": "to whom?"})
	wantCode(t, err, "INVALID_INPUT")

	f.ch.err = &domain.StructuredError{Code: "ARCHITECT_ONLY", Message: "only the architect replies"}
	_, err = f.call(t, "dlg", "reply_to_session", map[string]any{"session_id": "peer", "body": "hi"})
	wantCode(t, err, "ARCHITECT_ONLY")
}

func TestListTaskMessages_AsksForTheCallersView(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "dlg", "list_task_messages", map[string]any{"session_id": "arch", "since": "2026-10-07T10:00:00Z"})
	since, _ := time.Parse(time.RFC3339, "2026-10-07T10:00:00Z")
	if f.ch.caller != "dlg" || f.ch.filter.SessionID != "arch" || !f.ch.filter.Since.Equal(since) {
		t.Fatalf("viewer %q filter %+v", f.ch.caller, f.ch.filter)
	}
	if out["count"] != float64(1) || len(out["messages"].([]any)) != 1 {
		t.Fatalf("out = %+v", out)
	}

	f.ch.calls = nil
	_, err := f.call(t, "dlg", "list_task_messages", map[string]any{"since": "yesterday"})
	wantCode(t, err, "INVALID_INPUT")
	if len(f.ch.calls) != 0 {
		t.Fatalf("port called with a bad since: %v", f.ch.calls)
	}
}

func TestReviewTools_ActAsTheCaller(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "arch", "request_user_review", map[string]any{
		"subject": "Plan: tools", "body": "the plan crosses the contract", "about_session_id": "dlg",
		"document_ids": []any{"d1"}, "artifact_ids": []any{"a1"},
	})
	if f.ch.caller != "arch" || f.ch.review.Subject != "Plan: tools" || f.ch.review.AboutSessionID != "dlg" ||
		!slices.Equal(f.ch.review.DocumentIDs, []string{"d1"}) || !slices.Equal(f.ch.review.ArtifactIDs, []string{"a1"}) {
		t.Fatalf("review %+v from %q", f.ch.review, f.ch.caller)
	}
	if out["review_request"].(map[string]any)["id"] != "r1" {
		t.Fatalf("out = %+v", out)
	}

	out = f.mustCall(t, "arch", "withdraw_user_review", map[string]any{"review_id": "r1"})
	if f.ch.caller != "arch" || f.ch.target != "r1" || out["review_request"].(map[string]any)["state"] != "withdrawn" {
		t.Fatalf("withdraw: %+v", out)
	}

	out = f.mustCall(t, "dlg", "list_review_requests", map[string]any{"state": "pending"})
	if f.ch.state != domain.ReviewPending || out["count"] != float64(1) {
		t.Fatalf("list: state %q out %+v", f.ch.state, out)
	}
	_, err := f.call(t, "dlg", "list_review_requests", map[string]any{"state": "maybe"})
	wantCode(t, err, "INVALID_INPUT")

	f.ch.err = &domain.StructuredError{Code: "REVIEW_NOT_PENDING", Message: "already answered"}
	_, err = f.call(t, "arch", "withdraw_user_review", map[string]any{"review_id": "r1"})
	wantCode(t, err, "REVIEW_NOT_PENDING")
}

func TestSetStatusCheck_RequiresTheInterval(t *testing.T) {
	f := newChannelFixture(t)
	_, err := f.call(t, "arch", "set_status_check", map[string]any{"session_id": "dlg"})
	wantCode(t, err, "INVALID_INPUT")
	if f.ch.called("SetStatusCheck") {
		t.Fatal("a missing interval reached the port")
	}

	// 0 pauses: it is a value, not a missing argument.
	out := f.mustCall(t, "arch", "set_status_check", map[string]any{"session_id": "dlg", "every_minutes": 0})
	if f.ch.caller != "arch" || f.ch.target != "dlg" || f.ch.every != 0 {
		t.Fatalf("set %d on %q by %q", f.ch.every, f.ch.target, f.ch.caller)
	}
	if out["status_check"].(map[string]any)["delegate_session_id"] != "dlg" {
		t.Fatalf("out = %+v", out)
	}
	f.mustCall(t, "arch", "set_status_check", map[string]any{"session_id": "dlg", "every_minutes": 30})
	if f.ch.every != 30 {
		t.Fatalf("every = %d, want 30", f.ch.every)
	}
}

func TestListStatusChecks_IsTheArchitectsOnly(t *testing.T) {
	f := newChannelFixture(t)
	f.ch.checks = []*domain.StatusCheck{{DelegateSessionID: "dlg", EveryMinutes: 10, State: domain.StatusCheckActive}}
	_, err := f.call(t, "dlg", "list_status_checks", nil)
	wantCode(t, err, "ARCHITECT_ONLY")
	if f.ch.called("ListStatusChecks") {
		t.Fatal("a delegate's call reached ListStatusChecks")
	}
	out := f.mustCall(t, "arch", "list_status_checks", nil)
	if out["count"] != float64(1) {
		t.Fatalf("out = %+v", out)
	}
}

func TestUpdateTaskStatus_GoesThroughTheChannelWithAReason(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "arch", "update_task_status", map[string]any{"status": "review", "reason": "every delegate reported done"})
	if f.ch.caller != "arch" || f.ch.status != domain.TicketStatusReview || f.ch.reason != "every delegate reported done" {
		t.Fatalf("set %q by %q because %q", f.ch.status, f.ch.caller, f.ch.reason)
	}
	if out["task"].(map[string]any)["status"] != "review" {
		t.Fatalf("out = %+v", out)
	}

	f.ch.calls = nil
	_, err := f.call(t, "arch", "update_task_status", map[string]any{"status": "done", "reason": strings.Repeat("x", domain.MaxStatusReasonLen+1)})
	wantCode(t, err, "INVALID_INPUT")
	if len(f.ch.calls) != 0 {
		t.Fatalf("an overlong reason reached the port: %v", f.ch.calls)
	}

	f.ch.err = &domain.StructuredError{Code: "TASK_STATUS_OWNED_BY_ARCHITECT", Message: "the architect owns it"}
	_, err = f.call(t, "dlg", "update_task_status", map[string]any{"status": "done"})
	wantCode(t, err, "TASK_STATUS_OWNED_BY_ARCHITECT")
}

func TestListTaskSessions_ReportsRoles(t *testing.T) {
	f := newChannelFixture(t)
	out := f.mustCall(t, "dlg", "list_task_sessions", nil)
	if out["architect_session_id"] != "arch" {
		t.Fatalf("architect_session_id = %v", out["architect_session_id"])
	}
	if you := out["you"].(map[string]any); you["role"] != "delegate" {
		t.Fatalf("you = %+v", you)
	}
	roles := map[string]any{}
	for _, s := range out["sessions"].([]any) {
		s := s.(map[string]any)
		roles[s["session_id"].(string)] = s["role"]
	}
	if roles["arch"] != "architect" || roles["peer"] != "" {
		t.Fatalf("roles = %v", roles)
	}

	f.ch.architect = ""
	f.ch.roles = nil
	out = f.mustCall(t, "peer", "list_task_sessions", nil)
	if v, ok := out["architect_session_id"]; !ok || v != nil {
		t.Fatalf("no architect: architect_session_id = %v (present %v), want null", v, ok)
	}
}

// Without a channel the listing still answers, every session a peer.
func TestListTaskSessions_WithoutAChannelEverySessionIsAPeer(t *testing.T) {
	call := peersFixture(t)
	out := call("me", nil)
	if v, ok := out["architect_session_id"]; !ok || v != nil {
		t.Fatalf("architect_session_id = %v (present %v), want null", v, ok)
	}
	for _, s := range out["sessions"].([]any) {
		if role, ok := s.(map[string]any)["role"]; !ok || role != "" {
			t.Fatalf("session %v: role %v (present %v), want \"\"", s, role, ok)
		}
	}
}

func TestStartTaskSession_PassesTheStatusCheckInterval(t *testing.T) {
	f := newChannelFixture(t)
	f.mustCall(t, "arch", "start_task_session", map[string]any{"prompt": "plan the tools"})
	if got := f.starter.got[0].StatusCheckMinutes; got != nil {
		t.Fatalf("absent interval: StatusCheckMinutes = %d, want nil (the server's default)", *got)
	}
	for i, n := range []int{5, 0} {
		f.mustCall(t, "arch", "start_task_session", map[string]any{"prompt": "plan", "status_check_minutes": n})
		if got := f.starter.got[i+1].StatusCheckMinutes; got == nil || *got != n {
			t.Fatalf("StatusCheckMinutes = %v, want %d", got, n)
		}
	}
	for _, n := range []int{1, 241} {
		_, err := f.call(t, "arch", "start_task_session", map[string]any{"prompt": "plan", "status_check_minutes": n})
		wantCode(t, err, "INVALID_INPUT")
	}
	if len(f.starter.got) != 3 {
		t.Fatalf("an out-of-range interval started a session: %d starts", len(f.starter.got))
	}
}

func TestStartTaskSession_ReturnsTheArchitectsStatusCheck(t *testing.T) {
	f := newChannelFixture(t)
	// The server creates the loop inside StartInteractive; the fake names the
	// session the starter will create.
	f.ch.checks = []*domain.StatusCheck{
		{DelegateSessionID: "someone-else", EveryMinutes: 10},
		{DelegateSessionID: "peer-1", EveryMinutes: 10, State: domain.StatusCheckActive},
	}
	out := f.mustCall(t, "arch", "start_task_session", map[string]any{"prompt": "plan the tools"})
	check, _ := out["status_check"].(map[string]any)
	if check["delegate_session_id"] != "peer-1" {
		t.Fatalf("status_check = %+v", out["status_check"])
	}

	// A delegate's sessions get no loop; a value it passes is ignored, and said so.
	out = f.mustCall(t, "dlg", "start_task_session", map[string]any{"prompt": "write the tests", "status_check_minutes": 5})
	if _, ok := out["status_check"]; ok {
		t.Fatalf("a delegate's start reported a status check: %+v", out)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "status_check_minutes") {
		t.Fatalf("note = %q, want it to say status_check_minutes was ignored", note)
	}
}

func equalMessage(a, b ports.TaskMessageInput) bool {
	return a.Kind == b.Kind && a.Subject == b.Subject && a.Body == b.Body && a.Status == b.Status &&
		a.Verdict == b.Verdict && a.InReplyTo == b.InReplyTo &&
		slices.Equal(a.DocumentIDs, b.DocumentIDs) && slices.Equal(a.ArtifactIDs, b.ArtifactIDs)
}
