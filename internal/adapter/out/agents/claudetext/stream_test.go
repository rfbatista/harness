package claudetext

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// fakeStream stands in for llmkit's OneShotStream: it remembers the request,
// calls onText with each piece and answers with their concatenation.
type fakeStream struct {
	got    []claude.OneShotRequest
	pieces []string
	err    error
}

func (f *fakeStream) OneShotStream(ctx context.Context, req claude.OneShotRequest, onText func(string)) (claude.OneShotResult, error) {
	f.got = append(f.got, req)
	if f.err != nil {
		return claude.OneShotResult{}, f.err
	}
	text := ""
	for _, p := range f.pieces {
		onText(p)
		text += p
	}
	return claude.OneShotResult{Text: text}, nil
}

func TestAGatedStreamIsRefusedWithTheSameErrorAndNeverRunsTheCLI(t *testing.T) {
	refusal := errors.New("not ready")
	f := &fakeStream{pieces: []string{"a"}}
	s := GatedStream(f, func(context.Context) error { return refusal })
	called := false
	_, err := s.OneShotStream(context.Background(), claude.OneShotRequest{}, func(string) { called = true })
	if !errors.Is(err, refusal) || len(f.got) != 0 || called {
		t.Fatalf("err = %v, runs = %d, onText called = %v; want the refusal and no run", err, len(f.got), called)
	}
}

func TestAGatedStreamThatPassesStreamsEveryPieceInOrder(t *testing.T) {
	f := &fakeStream{pieces: []string{"he", "llo"}}
	s := GatedStream(f, func(context.Context) error { return nil })
	var got []string
	res, err := s.OneShotStream(context.Background(), claude.OneShotRequest{}, func(p string) { got = append(got, p) })
	if err != nil || res.Text != "hello" || !slices.Equal(got, []string{"he", "llo"}) {
		t.Fatalf("res = %q, pieces = %v, err = %v", res.Text, got, err)
	}
}

func TestAGatedStreamWithNoCheckLetsEverythingThrough(t *testing.T) {
	f := &fakeStream{}
	if GatedStream(f, nil) != StreamOneShotter(f) {
		t.Fatal("a nil check should return the inner streamer")
	}
}

func TestConfinedStreamGivesTheCredentialEnvAndTheWrapPreparedForIt(t *testing.T) {
	f := &fakeStream{pieces: []string{"x"}}
	wrap := llmkit.WrapFunc(func(c llmkit.Command) (llmkit.Command, error) { return c, nil })
	s := ConfinedStream(f, func() ([]string, llmkit.WrapFunc, error) { return []string{"K=v"}, wrap, nil })
	if _, err := s.OneShotStream(context.Background(), claude.OneShotRequest{Env: []string{"A=b"}}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	r := f.got[0]
	if !slices.Equal(r.Env, []string{"A=b", "K=v"}) || r.Wrap == nil {
		t.Fatalf("request = %+v; want the credential env appended and a wrap", r)
	}
}

func TestConfinedStreamRefusesWithThePreparationErrorBeforeTheCLIRuns(t *testing.T) {
	boom := errors.New("no sandbox")
	f := &fakeStream{}
	s := ConfinedStream(f, func() ([]string, llmkit.WrapFunc, error) { return nil, nil, boom })
	if _, err := s.OneShotStream(context.Background(), claude.OneShotRequest{}, func(string) {}); !errors.Is(err, boom) || len(f.got) != 0 {
		t.Fatalf("err = %v, runs = %d", err, len(f.got))
	}
}
