package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/application/archassistant"
)

type recordingTurn struct {
	got     claude.TurnRequest
	deleted []string
}

func (r *recordingTurn) TurnStream(_ context.Context, req claude.TurnRequest, on claude.TurnCallbacks) (claude.TurnResult, error) {
	r.got = req
	on.OnTool("Read", []byte(`{"file_path":"/work/harness/internal/domain/zone.go"}`))
	on.OnTool("Grep", []byte(`{"pattern":"ArchLayer","path":"/work/harness/internal"}`))
	on.OnText("hello")
	return claude.TurnResult{SessionID: "s1", Text: "hello"}, nil
}

func (r *recordingTurn) DeleteTranscript(id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func TestTheAssistantsClaudeTurnIsReadOnlyResumedAndReportsWhatItReads(t *testing.T) {
	rec := &recordingTurn{}
	turn := assistantTurn{rec}
	var tools []string
	var text string
	res, err := turn.Run(context.Background(), archassistant.TurnCall{
		System: "sys", Input: "in", Model: "sonnet", Attribution: "harness:architecture-assistant:p1", Dir: "/work/harness", ResumeID: "s0",
	}, archassistant.TurnCallbacks{
		OnText: func(s string) { text += s },
		OnTool: func(tool, target string) { tools = append(tools, tool+" "+target) },
	})
	if err != nil || res.SessionID != "s1" || res.Text != "hello" || text != "hello" {
		t.Fatalf("res = %+v, text = %q, err = %v", res, text, err)
	}
	r := rec.got
	if !slices.Equal(r.Tools, []string{"Read", "Grep", "Glob"}) || r.Dir != "/work/harness" || r.ResumeID != "s0" || r.Model != "sonnet" || r.Attribution != "harness:architecture-assistant:p1" {
		t.Errorf("request = %+v", r)
	}
	if want := []string{"Read internal/domain/zone.go", "Grep ArchLayer in internal"}; !slices.Equal(tools, want) {
		t.Errorf("activity = %v, want %v", tools, want)
	}
	turn.DeleteTranscript("s1")
	if !slices.Equal(rec.deleted, []string{"s1"}) {
		t.Errorf("deleted = %v", rec.deleted)
	}
}

func TestTheAssistantNeverRunsWithoutASandboxDriver(t *testing.T) {
	baseCalled := false
	base := func(context.Context) error { baseCalled = true; return nil }

	refused := requireConfinement(base, func() bool { return false })
	err := refused(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sandbox driver") {
		t.Fatalf("err = %v, want the missing-driver refusal", err)
	}
	if baseCalled {
		t.Error("the readiness gate ran for a turn that cannot be confined")
	}

	allowed := requireConfinement(base, func() bool { return true })
	if err := allowed(context.Background()); err != nil || !baseCalled {
		t.Fatalf("err = %v, base called = %v; want the gate's own answer", err, baseCalled)
	}

	boom := errors.New("not signed in")
	if err := requireConfinement(func(context.Context) error { return boom }, func() bool { return true })(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the gate's refusal", err)
	}
	if err := requireConfinement(nil, func() bool { return true })(context.Background()); err != nil {
		t.Errorf("a nil gate: %v", err)
	}
}
