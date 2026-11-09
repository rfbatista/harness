//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	llminterview "github.com/rfbatista/llmkit/interview"

	"operators-mcp/internal/domain"
)

// mounted is llmkit's interview handler mounted as the server mounts it, on a
// test server of its own, with the URLs sessions are given.
func (e *signalsE2E) mounted() func(session string) string {
	e.t.Helper()
	mux := http.NewServeMux()
	mux.Handle(llminterview.PathPrefix, interviewHandler(e.svc))
	srv := httptest.NewServer(mux)
	e.t.Cleanup(srv.Close)
	return interviewURLs(srv.URL)
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// callAsk is an agent's tools/call of the ask tool; it returns the tool's
// text and whether the call was an error.
func callAsk(t *testing.T, url string, rpcID int, args map[string]any) (string, bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": rpcID, "method": "tools/call",
		"params": map[string]any{"name": llminterview.ToolName, "arguments": args}})
	status, out := post(t, url, string(body))
	if status != http.StatusOK {
		t.Fatalf("tools/call: %d %s", status, out)
	}
	if i := strings.Index(out, "data:"); i >= 0 { // an event-stream reply
		out = strings.TrimSpace(strings.SplitN(out[i+len("data:"):], "\n", 2)[0])
	}
	var rpc struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &rpc); err != nil || len(rpc.Result.Content) == 0 {
		t.Fatalf("tools/call reply %q: %v", out, err)
	}
	return rpc.Result.Content[0].Text, rpc.Result.IsError
}

func validAsk() map[string]any {
	return map[string]any{"title": "Storage", "questions": []any{
		map[string]any{"title": "Which store?", "kind": "single", "recommended_option": 1,
			"options": []any{map[string]any{"label": "SQLite", "description": "One file."}, map[string]any{"label": "Postgres"}}},
		map[string]any{"title": "Why?", "kind": "text", "recommended_text": "Because."},
	}}
}

const nativeAsk = `{"session_id":"abc","hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_use_id":"toolu_01",
"tool_input":{"questions":[{"question":"Which database?","header":"DB","multiSelect":false,
"options":[{"label":"SQLite","description":"embedded"},{"label":"Postgres","description":"server"}]}]}}`

var interviewID = regexp.MustCompile(`iv_[0-9a-f]+`)

func (e *signalsE2E) onlyInterview(session string) *domain.Interview {
	e.t.Helper()
	list := e.read(session)
	if len(list.Interviews) != 1 {
		e.t.Fatalf("interviews: %d, want 1: %+v", len(list.Interviews), list.Interviews)
	}
	return list.Interviews[0]
}

// The ask tool, over HTTP at the session's InterviewURL, ends in a stored
// interview that every surface of the session shows, with one notification.
func TestTheAskToolStoresAnInterviewOnTheLiveSession(t *testing.T) {
	e := newSignalsE2E(t)
	url := e.mounted()
	id := e.start(domain.SessionRunning)
	feed := e.projectFeed()

	text, isErr := callAsk(t, url(id), 1, validAsk())
	if isErr {
		t.Fatalf("the ask failed: %s", text)
	}
	got := e.onlyInterview(id)
	if !strings.Contains(text, got.ID) || !interviewID.MatchString(got.ID) {
		t.Errorf("reply %q does not name the stored interview %q", text, got.ID)
	}
	if got.State != domain.InterviewOpen || got.SessionID != id || got.Title != "Storage" || len(got.Questions) != 2 {
		t.Fatalf("stored: %+v", got)
	}
	q1, q2 := got.Questions[0], got.Questions[1]
	if q1.Kind != domain.QuestionSingle || len(q1.Options) != 2 || q1.Options[0].Description != "One file." ||
		q1.Recommended == nil || q1.Recommended.Option == nil || *q1.Recommended.Option != 1 || q1.Recommended.Text != nil {
		t.Errorf("question 1: %+v", q1)
	}
	if q2.Kind != domain.QuestionText || q2.Options == nil || len(q2.Options) != 0 ||
		q2.Recommended == nil || q2.Recommended.Text == nil || *q2.Recommended.Text != "Because." || q2.Recommended.Option != nil {
		t.Errorf("question 2: %+v", q2)
	}
	if s := e.session(id); s.OpenInterviewCount != 1 || s.OpenQuestionCount != 2 {
		t.Errorf("head counts: %+v", s)
	}
	feedSession(t, feed, id, 1, 2)
	if ns := e.notificationsOf(id); len(ns) != 1 {
		t.Errorf("notifications: %d, want 1", len(ns))
	}
}

// The native AskUserQuestion, captured, becomes the same stored interview and
// its refusal names the interview.
func TestACapturedAskUserQuestionStoresAnInterviewOnTheLiveSession(t *testing.T) {
	e := newSignalsE2E(t)
	url := e.mounted()
	id := e.start(domain.SessionRunning)
	feed := e.projectFeed()

	status, body := post(t, url(id)+llminterview.CapturePath, nativeAsk)
	if status != http.StatusOK {
		t.Fatalf("capture: %d %s", status, body)
	}
	got := e.onlyInterview(id)
	if !strings.Contains(body, got.ID) || !strings.Contains(body, "deny") {
		t.Errorf("the hook response %s does not refuse naming %s", body, got.ID)
	}
	if got.State != domain.InterviewOpen || len(got.Questions) != 1 || !strings.Contains(got.Questions[0].Title+got.Questions[0].Body, "Which database?") ||
		got.Questions[0].Kind != domain.QuestionSingle || len(got.Questions[0].Options) != 2 {
		t.Fatalf("stored: %+v", got)
	}
	if s := e.session(id); s.OpenInterviewCount != 1 || s.OpenQuestionCount != 1 {
		t.Errorf("head counts: %+v", s)
	}
	feedSession(t, feed, id, 1, 1)
	if ns := e.notificationsOf(id); len(ns) != 1 {
		t.Errorf("notifications: %d, want 1", len(ns))
	}

	// The hook retried with the same tool use is not a second interview.
	post(t, url(id)+llminterview.CapturePath, nativeAsk)
	e.onlyInterview(id)
	if ns := e.notificationsOf(id); len(ns) != 1 {
		t.Errorf("notifications after a retry: %d, want 1", len(ns))
	}
}

func TestARetriedAskToolCallIsNotASecondInterview(t *testing.T) {
	e := newSignalsE2E(t)
	url := e.mounted()
	id := e.start(domain.SessionRunning)

	callAsk(t, url(id), 7, validAsk())
	callAsk(t, url(id), 7, validAsk())

	e.onlyInterview(id)
	if ns := e.notificationsOf(id); len(ns) != 1 {
		t.Errorf("notifications: %d, want 1", len(ns))
	}
}

func TestAskingForAnEndedSessionIsUnavailableAndStoresNothing(t *testing.T) {
	e := newSignalsE2E(t)
	url := e.mounted()
	id := e.start(domain.SessionDone)

	text, isErr := callAsk(t, url(id), 1, validAsk())
	if !isErr || !strings.HasPrefix(text, "INTERVIEW_UNAVAILABLE") {
		t.Errorf("ask tool: error %v, %q", isErr, text)
	}
	status, _ := post(t, url(id)+llminterview.CapturePath, nativeAsk)
	if status != http.StatusServiceUnavailable {
		t.Errorf("capture status %d, want 503 (the hook then fails open)", status)
	}
	if list := e.read(id); len(list.Interviews) != 0 {
		t.Errorf("stored: %+v", list.Interviews)
	}
	if ns := e.notificationsOf(id); len(ns) != 0 {
		t.Errorf("notifications: %d", len(ns))
	}
}

func TestAnInvalidInterviewIsRefusedAndNeverStored(t *testing.T) {
	e := newSignalsE2E(t)
	url := e.mounted()
	id := e.start(domain.SessionRunning)

	args := map[string]any{"questions": []any{
		map[string]any{"title": "Which?", "kind": "single", "options": []any{map[string]any{"label": "only one"}}},
	}}
	text, isErr := callAsk(t, url(id), 1, args)
	if !isErr || !strings.HasPrefix(text, "INTERVIEW_INVALID") {
		t.Errorf("ask tool: error %v, %q", isErr, text)
	}
	if list := e.read(id); len(list.Interviews) != 0 {
		t.Errorf("stored: %+v", list.Interviews)
	}
}

// Every session is given its interview URL under the loopback base, named by
// the harness session id.
func TestSessionsGetTheirInterviewURLFromTheLoopbackServer(t *testing.T) {
	url := interviewURLs("http://127.0.0.1:8080")
	if got, want := url("s1"), "http://127.0.0.1:8080"+llminterview.PathPrefix+"s1"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
}
