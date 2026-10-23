package claudetext

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// The text-processing ports over llmkit's one-shot mode: each adapter owns
// its prompt, and every call is the harness's own spend.

func TestSummarizeSendsTheTextAsTheInputUnderTheSummarizerPrompt(t *testing.T) {
	f := &fakeOneShot{result: claudeResult("the gist")}
	out, err := NewSummarizer(f, "claude", "sonnet").Summarize(context.Background(), "a long piece of prose")
	if err != nil || out != "the gist" {
		t.Fatalf("Summarize = %q, %v; want the one-shot's answer", out, err)
	}
	req := f.got[0]
	if req.Input != "a long piece of prose" || !strings.Contains(req.System, "summarizer") || req.Model != "sonnet" {
		t.Errorf("request = %+v, want the text as input, the summarizer prompt and model sonnet", req)
	}
	if req.Attribution != "harness:system" {
		t.Errorf("attribution = %q, want harness:system", req.Attribution)
	}
}

func TestGenerateAndTranslateCarryTheirOwnPrompts(t *testing.T) {
	f := &fakeOneShot{}
	if _, err := NewTextGenerator(f, "claude", "").GenerateText(context.Background(), "write a haiku"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTranslator(f, "claude", "").Translate(context.Background(), "Bom dia", "  Japanese  "); err != nil {
		t.Fatal(err)
	}
	gen, tr := f.got[0], f.got[1]
	if gen.Input != "write a haiku" || !strings.Contains(gen.System, "text generator") || gen.Model != "" || gen.Attribution != "harness:system" {
		t.Errorf("generate request = %+v, want the prompt as input, the generator prompt, no model, harness:system", gen)
	}
	if tr.Input != "Bom dia" || !strings.Contains(tr.System, "into Japanese") || strings.Contains(tr.Input, "Japanese") || tr.Attribution != "harness:system" {
		t.Errorf("translate request = %+v, want the source text alone as input and the trimmed language in the prompt", tr)
	}
}

func TestBlankInputIsRejectedBeforeCallingClaude(t *testing.T) {
	f := &fakeOneShot{}
	s, g, tr := NewSummarizer(f, "claude", ""), NewTextGenerator(f, "claude", ""), NewTranslator(f, "claude", "")
	for name, call := range map[string]func() (string, error){
		"summarize blank text":     func() (string, error) { return s.Summarize(context.Background(), "  \n ") },
		"generate blank prompt":    func() (string, error) { return g.GenerateText(context.Background(), "") },
		"translate blank text":     func() (string, error) { return tr.Translate(context.Background(), " ", "French") },
		"translate blank language": func() (string, error) { return tr.Translate(context.Background(), "hello", " ") },
	} {
		_, err := call()
		var se *domain.StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
			t.Errorf("%s: err = %v, want INVALID_INPUT", name, err)
		}
	}
	if len(f.got) != 0 {
		t.Errorf("claude was called %d times, want none", len(f.got))
	}
}

func TestAMissingCLIIsAStructuredError(t *testing.T) {
	f := &fakeOneShot{err: fmt.Errorf("claude: start: %w", exec.ErrNotFound)}
	_, err := NewSummarizer(f, "definitely-not-claude", "").Summarize(context.Background(), "text")
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "CLAUDE_CLI_NOT_FOUND" || !strings.Contains(se.Message, "definitely-not-claude") {
		t.Fatalf("err = %v, want CLAUDE_CLI_NOT_FOUND naming the binary", err)
	}
}

func TestCLIFailuresAndCancellationSurface(t *testing.T) {
	f := &fakeOneShot{err: errors.New("claude: error_max_turns: turn limit")}
	if _, err := NewSummarizer(f, "claude", "").Summarize(context.Background(), "text"); err == nil || !strings.Contains(err.Error(), "error_max_turns") {
		t.Errorf("err = %v, want the CLI's failure", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSummarizer(&fakeOneShot{}, "claude", "").Summarize(ctx, "text"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAGatedOneShotIsRefusedWithTheSameErrorAndNeverRunsTheCLI(t *testing.T) {
	refusal := &domain.StructuredError{Code: "AGENT_CREDENTIAL_MISSING", Message: "Agent not started: no agent credential. (/agent-isolation#credential)"}
	fake := &fakeOneShot{}
	missing := true
	c := Gated(fake, func(context.Context) error {
		if missing {
			return refusal
		}
		return nil
	})
	ctx := context.Background()
	if _, err := NewSummarizer(c, "claude", "").Summarize(ctx, "text"); err != error(refusal) {
		t.Errorf("summary err = %v", err)
	}
	if _, err := NewTranslator(c, "claude", "").Translate(ctx, "text", "French"); err != error(refusal) {
		t.Errorf("translation err = %v", err)
	}
	if _, err := NewTextGenerator(c, "claude", "").GenerateText(ctx, "write"); err != error(refusal) {
		t.Errorf("generation err = %v", err)
	}
	if len(fake.got) != 0 {
		t.Fatalf("the CLI ran %d times for refused one-shots", len(fake.got))
	}
	missing = false
	if out, err := NewSummarizer(c, "claude", "").Summarize(ctx, "text"); err != nil || out != "done" {
		t.Fatalf("after the prerequisite is met: %q, %v", out, err)
	}
}

func TestConfinedGivesEveryOneShotTheCredentialEnvAndTheWrapPreparedForIt(t *testing.T) {
	f := &fakeOneShot{}
	wrapped := false
	wrap := func(c llmkit.Command) (llmkit.Command, error) { wrapped = true; return c, nil }
	one := Confined(f, func() ([]string, llmkit.WrapFunc, error) {
		return []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}, wrap, nil
	})
	if _, err := NewSummarizer(one, "claude", "").Summarize(context.Background(), "text"); err != nil {
		t.Fatal(err)
	}
	req := f.got[0]
	if len(req.Env) != 1 || req.Env[0] != "CLAUDE_CODE_OAUTH_TOKEN=tok" || req.Wrap == nil {
		t.Fatalf("request = %+v, want the credential env and a wrap", req)
	}
	_, _ = req.Wrap(llmkit.Command{})
	if !wrapped || req.Attribution != "harness:system" || req.Input != "text" {
		t.Errorf("wrapped=%v request=%+v, want the prepared wrap and the request otherwise unchanged", wrapped, req)
	}
}

func TestConfinedRefusesWithThePreparationErrorBeforeTheCLIRuns(t *testing.T) {
	f := &fakeOneShot{}
	boom := errors.New("no sandbox driver")
	one := Confined(f, func() ([]string, llmkit.WrapFunc, error) { return nil, nil, boom })
	if _, err := NewTranslator(one, "claude", "").Translate(context.Background(), "hi", "French"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the preparation error", err)
	}
	if len(f.got) != 0 {
		t.Errorf("claude was called %d times, want none", len(f.got))
	}
}

func TestConfinedWithoutAWrapRunsTheCommandAsItWas(t *testing.T) {
	f := &fakeOneShot{}
	one := Confined(f, func() ([]string, llmkit.WrapFunc, error) { return nil, nil, nil })
	if _, err := NewTextGenerator(one, "claude", "").GenerateText(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if f.got[0].Wrap != nil {
		t.Errorf("Wrap = non-nil, want none with the sandbox off")
	}
}
