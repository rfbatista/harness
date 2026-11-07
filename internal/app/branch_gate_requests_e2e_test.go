//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The branch gate's asking hook and the person's answers, over HTTP with
// claude-shaped hook input, with the real shim then making the action
// (contract "Harness server ↔ Agent sessions — branch gate" §2 and "Harness
// server ↔ Web UI — branch requests and the Branches list" §2).

// hookAnswer is what the asking hook answered.
type hookAnswer struct {
	code int
	body string
}

// decision is the permissionDecision of the answer, "" for an empty one.
func (a hookAnswer) decision() string {
	var out struct {
		Hook struct {
			Decision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	_ = json.Unmarshal([]byte(a.body), &out)
	return out.Hook.Decision
}

// askHook posts Bash hook input for command and answers a channel that gets
// the answer once the hook returns.
func (g *branchGate) askHook(command, description string) <-chan hookAnswer {
	g.t.Helper()
	in := map[string]any{"tool_name": "Bash", "cwd": g.worktree, "tool_input": map[string]any{"command": command, "description": description}}
	b, _ := json.Marshal(in)
	out := make(chan hookAnswer, 1)
	go func() {
		res, err := http.Post(g.srv.URL+"/api/sessions/"+g.id+"/branch_gate?hook=PreToolUse", "application/json", bytes.NewReader(b))
		if err != nil {
			out <- hookAnswer{code: -1, body: err.Error()}
			return
		}
		defer res.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(res.Body)
		out <- hookAnswer{res.StatusCode, buf.String()}
	}()
	return out
}

// requests lists the session's branch requests, newest first.
func (g *branchGate) requests(state string) []domain.BranchRequest {
	g.t.Helper()
	url := g.srv.URL + "/api/branch_requests?session_id=" + g.id
	if state != "" {
		url += "&state=" + state
	}
	res, err := http.Get(url)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Requests []domain.BranchRequest `json:"requests"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		g.t.Fatal(err)
	}
	return out.Requests
}

// pending waits for n pending requests, oldest first.
func (g *branchGate) pending(n int) []domain.BranchRequest {
	g.t.Helper()
	var got []domain.BranchRequest
	eventually(g.t, fmt.Sprintf("%d pending branch requests", n), func() error {
		got = g.requests("pending")
		if len(got) != n {
			return fmt.Errorf("%d pending", len(got))
		}
		return nil
	})
	for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 {
		got[i], got[j] = got[j], got[i]
	}
	return got
}

// answer posts the person's answer; it returns the status and the body.
func (g *branchGate) answer(id, decision, note string) (int, string) {
	g.t.Helper()
	b, _ := json.Marshal(map[string]string{"decision": decision, "note": note})
	res, err := http.Post(g.srv.URL+"/api/branch_requests/"+id+"/answer", "application/json", bytes.NewReader(b))
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.String()
}

func (g *branchGate) status() domain.SessionStatus {
	g.t.Helper()
	return g.sessions.Get(g.id).Status
}

// waits asserts the hook has not answered yet.
func waits(t *testing.T, ch <-chan hookAnswer) {
	t.Helper()
	select {
	case a := <-ch:
		t.Fatalf("the hook answered before the person did: %+v", a)
	case <-time.After(300 * time.Millisecond):
	}
}

func answered(t *testing.T, ch <-chan hookAnswer) hookAnswer {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(5 * time.Second):
		t.Fatal("the hook did not answer")
		return hookAnswer{}
	}
}

func TestABranchActionWaitsForTheAnswerAndTheApprovalIsOneGrant(t *testing.T) {
	g := startBranchGate(t)
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	before := g.status()
	hook := g.askHook("git checkout -b feature-x", "Start the work on feature-x")
	req := g.pending(1)[0]
	waits(t, hook)

	if req.Action != domain.BranchCreate || req.Branch != "feature-x" || req.Reason != "Start the work on feature-x" || req.Via != "git" ||
		req.Command != "git checkout -b feature-x" || req.SessionBranch != session || req.SessionID != g.id || req.State != domain.BranchRequestPending ||
		req.StartPoint == nil || req.StartPoint.Kind != "branch" || req.StartPoint.Ref != session || req.TaskID == nil || req.RepositoryID != g.repo {
		t.Errorf("request = %+v", req)
	}
	if got := g.status(); got != domain.SessionWaitingApproval {
		t.Errorf("status while pending = %s, want waiting_approval", got)
	}

	if code, body := g.answer(req.ID, "approve", "ok, but keep it small"); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	a := answered(t, hook)
	if a.decision() != "allow" || !strings.Contains(a.body, "ok, but keep it small") {
		t.Errorf("hook answer = %+v", a)
	}
	if got := g.status(); got != before {
		t.Errorf("status after the answer = %s, want %s", got, before)
	}

	// The approval is one grant, for exactly that action.
	if r := g.git("git checkout -b feature-y"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("another branch with the grant out: %+v", r)
	}
	if r := g.git("git checkout -b feature-x"); r.code != 0 {
		t.Fatalf("the approved command: %+v", r)
	}
	g.setup("checkout " + session)
	if r := g.git("git checkout -b feature-x"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("the grant was used twice: %+v", r)
	}
	if used := g.requests("used"); len(used) != 1 || used[0].ID != req.ID {
		t.Errorf("used requests = %+v", used)
	}
}

func TestADenialGivesTheAgentTheNoteAndNoGrant(t *testing.T) {
	g := startBranchGate(t)
	hook := g.askHook("git branch -D to-delete", "")
	req := g.pending(1)[0]
	if req.Action != domain.BranchDelete || req.Branch != "to-delete" {
		t.Fatalf("request = %+v", req)
	}
	if code, body := g.answer(req.ID, "deny", "keep that one"); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	a := answered(t, hook)
	want := fmt.Sprintf("The person denied branch request %s (delete to-delete). Note: keep that one", req.ID)
	if a.decision() != "deny" || !strings.Contains(a.body, want) {
		t.Errorf("hook answer = %+v, want %q", a, want)
	}
	if r := g.git("git branch -D to-delete"); r.code != 1 || !strings.Contains(r.stderr, needed) {
		t.Errorf("a denied delete ran: %+v", r)
	}
	if got := g.requests("denied"); len(got) != 1 || got[0].Note != "keep that one" {
		t.Errorf("denied = %+v", got)
	}
}

func TestACommandWithTwoBranchActionsRunsOnlyWhenBothAreApproved(t *testing.T) {
	g := startBranchGate(t)
	hook := g.askHook("git branch a && git branch b", "")
	two := g.pending(2)
	if two[0].Branch != "a" || two[1].Branch != "b" {
		t.Fatalf("requests = %+v", two)
	}
	g.answer(two[0].ID, "approve", "")
	waits(t, hook)
	g.answer(two[1].ID, "approve", "")
	if a := answered(t, hook); a.decision() != "allow" {
		t.Errorf("hook answer = %+v", a)
	}
	if r := g.git("git branch a && git branch b"); r.code != 0 {
		t.Errorf("both approved: %+v", r)
	}

	hook = g.askHook("git branch c && git branch d", "")
	two = g.pending(2)
	g.answer(two[0].ID, "approve", "")
	g.answer(two[1].ID, "deny", "no d")
	if a := answered(t, hook); a.decision() != "deny" || !strings.Contains(a.body, "no d") {
		t.Errorf("one denial must deny the command: %+v", a)
	}
}

func TestACommandWithNoBranchActionIsAnsweredAtOnceWithNoRequest(t *testing.T) {
	g := startBranchGate(t)
	session := strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
	for _, command := range []string{"ls -la", "git status && git log --oneline", "git checkout " + session, "git checkout -- a.txt", "echo git checkout -b x", "git commit -m 'git branch x'", "make"} {
		a := answered(t, g.askHook(command, ""))
		if a.code != http.StatusOK || a.body != "" {
			t.Errorf("%q: %+v, want an empty answer", command, a)
		}
	}
	if n := len(g.requests("")); n != 0 {
		t.Errorf("%d requests filed", n)
	}
}

func TestOnlyAPendingRequestIsAnswered(t *testing.T) {
	g := startBranchGate(t)
	hook := g.askHook("git branch z", "")
	req := g.pending(1)[0]
	g.answer(req.ID, "approve", "")
	answered(t, hook)
	if code, body := g.answer(req.ID, "deny", ""); code != http.StatusConflict || !strings.Contains(body, "BRANCH_REQUEST_ANSWERED") {
		t.Errorf("a second answer: %d %s", code, body)
	}
	if code, body := g.answer("nope", "approve", ""); code != http.StatusNotFound || !strings.Contains(body, "BRANCH_REQUEST_NOT_FOUND") {
		t.Errorf("an unknown request: %d %s", code, body)
	}
	if code, _ := g.answer(req.ID, "maybe", ""); code != http.StatusBadRequest {
		t.Errorf("a decision that is neither: %d", code)
	}
}
