package usage_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/usage/usagetest"
	"operators-mcp/internal/ports"
)

// A writing helper step is attributed "harness:writing-helper:<project id>".
// It is stored on that project, with no session or task, as plain
// "harness:writing-helper".
func TestAWritingHelperCallIsStoredOnItsProjectWithNoSession(t *testing.T) {
	e := usagetest.NewEnv(t)
	now := time.Now()
	e.Service.Record(context.Background(), ports.UsageRequest{
		ID: "wh1", Attribution: "harness:writing-helper:" + e.ProjectA, Method: "POST", Path: "/v1/messages",
		Model: "claude-haiku", Status: 200, StartedAt: now, EndedAt: now.Add(time.Second), InputTokens: 10, OutputTokens: 5,
	})
	got, err := e.Repo.ListRecent(ports.UsageFilter{ProjectID: e.ProjectA, Limit: 10})
	if err != nil || len(got) != 1 {
		t.Fatalf("records = %v, err %v", got, err)
	}
	r := got[0]
	if r.ProjectID != e.ProjectA || r.SessionID != "" || r.TaskID != "" || r.Attribution != "harness:writing-helper" {
		t.Fatalf("record = %+v, want project %s, no session or task, attribution harness:writing-helper", r, e.ProjectA)
	}
}

func TestALiveWritingHelperCallHasItsOwnKindAndProject(t *testing.T) {
	e := usagetest.NewEnv(t)
	mine, other := e.Service.OpenLiveCalls(e.ProjectA), e.Service.OpenLiveCalls(e.ProjectB)
	defer mine.Close()
	defer other.Close()
	e.Service.Started(context.Background(), ports.CapturedRequest{
		ID: "wh1", Attribution: "harness:writing-helper:" + e.ProjectA, Method: "POST", Path: "/v1/messages",
		Header: http.Header{}, Body: []byte(`{}`), StartedAt: time.Now(),
	})
	ev, ok := mine.Take()
	if !ok {
		t.Fatal("project A's viewer saw nothing")
	}
	b, _ := ev.Data.(interface{ MarshalJSON() ([]byte, error) }).MarshalJSON()
	for _, want := range []string{`"kind":"writing-helper"`, `"project_id":"` + e.ProjectA + `"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("event %s lacks %s", b, want)
		}
	}
	if _, ok := other.Take(); ok {
		t.Error("project B's viewer saw project A's call")
	}
}
