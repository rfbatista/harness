package claudetext

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// fakeTurn stands in for llmkit's TurnStream: it remembers the request, reports
// a tool use and a text piece, and answers with a session id.
type fakeTurn struct {
	got     []claude.TurnRequest
	err     error
	deleted []string
}

func (f *fakeTurn) TurnStream(_ context.Context, req claude.TurnRequest, on claude.TurnCallbacks) (claude.TurnResult, error) {
	f.got = append(f.got, req)
	if f.err != nil {
		return claude.TurnResult{}, f.err
	}
	on.OnTool("Read", []byte(`{"file_path":"/work/harness/internal/domain/zone.go"}`))
	on.OnText("hi")
	return claude.TurnResult{SessionID: "s1", Text: "hi"}, nil
}

func (f *fakeTurn) DeleteTranscript(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func TestAGatedTurnIsRefusedWithTheSameErrorAndNeverRunsTheCLI(t *testing.T) {
	refusal := errors.New("not ready")
	f := &fakeTurn{}
	tr := GatedTurn(f, func(context.Context) error { return refusal })
	if _, err := tr.TurnStream(context.Background(), claude.TurnRequest{}, claude.TurnCallbacks{}); !errors.Is(err, refusal) || len(f.got) != 0 {
		t.Fatalf("err = %v, runs = %d; want the refusal and no run", err, len(f.got))
	}
}

func TestAGatedTurnThatPassesRunsAndDeletesTranscripts(t *testing.T) {
	f := &fakeTurn{}
	tr := GatedTurn(f, func(context.Context) error { return nil })
	res, err := tr.TurnStream(context.Background(), claude.TurnRequest{}, claude.TurnCallbacks{OnTool: func(string, []byte) {}, OnText: func(string) {}})
	if err != nil || res.SessionID != "s1" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if err := tr.DeleteTranscript("s1"); err != nil || !slices.Equal(f.deleted, []string{"s1"}) {
		t.Fatalf("deleted = %v, err = %v", f.deleted, err)
	}
}

func TestConfinedTurnPreparesTheWrapForTheTurnsOwnDirectory(t *testing.T) {
	f := &fakeTurn{}
	wrap := llmkit.WrapFunc(func(c llmkit.Command) (llmkit.Command, error) { return c, nil })
	var preparedFor string
	tr := ConfinedTurn(f, func(dir string) ([]string, llmkit.WrapFunc, error) {
		preparedFor = dir
		return []string{"K=v"}, wrap, nil
	})
	req := claude.TurnRequest{Dir: "/work/harness", Env: []string{"A=b"}}
	if _, err := tr.TurnStream(context.Background(), req, claude.TurnCallbacks{OnTool: func(string, []byte) {}, OnText: func(string) {}}); err != nil {
		t.Fatal(err)
	}
	r := f.got[0]
	if preparedFor != "/work/harness" || !slices.Equal(r.Env, []string{"A=b", "K=v"}) || r.Wrap == nil {
		t.Fatalf("prepared for %q, request = %+v; want the dir, the credential env appended and a wrap", preparedFor, r)
	}
}

func TestConfinedTurnRefusesWithThePreparationErrorBeforeTheCLIRuns(t *testing.T) {
	boom := errors.New("no sandbox")
	f := &fakeTurn{}
	tr := ConfinedTurn(f, func(string) ([]string, llmkit.WrapFunc, error) { return nil, nil, boom })
	if _, err := tr.TurnStream(context.Background(), claude.TurnRequest{}, claude.TurnCallbacks{}); !errors.Is(err, boom) || len(f.got) != 0 {
		t.Fatalf("err = %v, runs = %d", err, len(f.got))
	}
}

func TestAToolUsesTargetIsWhatItWorksOn(t *testing.T) {
	for name, tc := range map[string]struct {
		tool, input, want string
	}{
		"read a file":                 {"Read", `{"file_path":"/work/harness/internal/domain/zone.go"}`, "/work/harness/internal/domain/zone.go"},
		"grep a pattern":              {"Grep", `{"pattern":"ArchLayer","path":"/work/harness/internal"}`, "ArchLayer in /work/harness/internal"},
		"grep with no path":           {"Grep", `{"pattern":"ArchLayer"}`, "ArchLayer"},
		"glob a pattern":              {"Glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		"glob in a path":              {"Glob", `{"pattern":"*.go","path":"/work/harness/cmd"}`, "*.go in /work/harness/cmd"},
		"an unknown tool":             {"Task", `{"prompt":"x"}`, ""},
		"input that is not an object": {"Read", `nope`, ""},
	} {
		if got := ToolTarget(tc.tool, []byte(tc.input)); got != tc.want {
			t.Errorf("%s: ToolTarget = %q, want %q", name, got, tc.want)
		}
	}
}

func TestATargetInsideTheCheckoutIsShownRelativeToIt(t *testing.T) {
	if got := RelativeTo("/work/harness", "/work/harness/internal/domain/zone.go"); got != "internal/domain/zone.go" {
		t.Errorf("got %q", got)
	}
	if got := RelativeTo("/work/harness", "ArchLayer in /work/harness/internal"); got != "ArchLayer in internal" {
		t.Errorf("got %q", got)
	}
	if got := RelativeTo("/work/harness", "see /work/harness/x in /work/harness/y"); got != "see /work/harness/x in y" {
		t.Errorf("a pattern that names the checkout is not a path: got %q", got)
	}
	if got := RelativeTo("/work/harness", "/etc/passwd"); got != "/etc/passwd" {
		t.Errorf("got %q", got)
	}
}
