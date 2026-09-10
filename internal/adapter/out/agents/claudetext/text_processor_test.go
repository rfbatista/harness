package claudetext

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// decodeEcho runs one adapter call against the fake one-shot CLI and returns
// what the fake saw: the argv, the piped prompt and the working directory.
func decodeEcho(t *testing.T, out string) oneShotEcho {
	t.Helper()
	var e oneShotEcho
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("decode echo from %q: %v", out, err)
	}
	return e
}

// flagValue returns the argument following flag in args.
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func TestSummarizeSendsTextOnStdin(t *testing.T) {
	bin, env := fakeOneShotCmd()
	s := &Summarizer{cli: oneShot{bin: bin, model: "sonnet", env: env}}

	out, err := s.Summarize(context.Background(), "a long piece of prose")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	echo := decodeEcho(t, out)

	if echo.Stdin != "a long piece of prose" {
		t.Errorf("stdin = %q, want the text verbatim", echo.Stdin)
	}
	if slices.Contains(echo.Args, "a long piece of prose") {
		t.Error("text leaked into argv; it must only travel on stdin")
	}
	sys, ok := flagValue(echo.Args, "--system-prompt")
	if !ok || !strings.Contains(sys, "summarizer") {
		t.Errorf("--system-prompt = %q, want the summarizer prompt", sys)
	}
	if model, _ := flagValue(echo.Args, "--model"); model != "sonnet" {
		t.Errorf("--model = %q, want sonnet", model)
	}
}

// The whole point of the one-shot adapter is that it is a pure function of its
// input: no tools, no MCP servers, no skills, no project context, no session
// left on disk. Losing any of these flags silently turns a text utility into a
// coding agent with filesystem access.
func TestOneShotIsSandboxedAndStateless(t *testing.T) {
	bin, env := fakeOneShotCmd()
	g := &TextGenerator{cli: oneShot{bin: bin, env: env}}

	out, err := g.GenerateText(context.Background(), "write a haiku")
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	echo := decodeEcho(t, out)

	for _, flag := range []string{
		"--print", "--strict-mcp-config", "--disable-slash-commands",
		"--no-session-persistence", "--safe-mode",
	} {
		if !slices.Contains(echo.Args, flag) {
			t.Errorf("missing %s in %v", flag, echo.Args)
		}
	}
	if v, _ := flagValue(echo.Args, "--tools"); v != "" {
		t.Errorf("--tools = %q, want the empty string (all tools disabled)", v)
	}
	if v, _ := flagValue(echo.Args, "--output-format"); v != "json" {
		t.Errorf("--output-format = %q, want json", v)
	}
	if v, _ := flagValue(echo.Args, "--max-turns"); v != "1" {
		t.Errorf("--max-turns = %q, want 1", v)
	}
	if _, ok := flagValue(echo.Args, "--model"); ok {
		t.Error("--model passed although no model was configured")
	}
	if strings.Contains(echo.Dir, "coding_pool") {
		t.Errorf("ran in %q, want a neutral directory outside the project", echo.Dir)
	}
}

func TestTranslatePutsTargetLanguageInSystemPrompt(t *testing.T) {
	bin, env := fakeOneShotCmd()
	tr := &Translator{cli: oneShot{bin: bin, env: env}}

	out, err := tr.Translate(context.Background(), "Bom dia", "  Japanese  ")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	echo := decodeEcho(t, out)

	sys, _ := flagValue(echo.Args, "--system-prompt")
	if !strings.Contains(sys, "into Japanese") {
		t.Errorf("--system-prompt = %q, want the trimmed target language in it", sys)
	}
	if echo.Stdin != "Bom dia" {
		t.Errorf("stdin = %q, want the source text alone", echo.Stdin)
	}
	if strings.Contains(echo.Stdin, "Japanese") {
		t.Error("target language leaked into the translated text")
	}
}

func TestBlankInputIsRejectedBeforeSpawning(t *testing.T) {
	// A binary that does not exist: reaching it at all would be the failure.
	s := &Summarizer{cli: oneShot{bin: "/nonexistent/claude"}}
	g := &TextGenerator{cli: oneShot{bin: "/nonexistent/claude"}}
	tr := &Translator{cli: oneShot{bin: "/nonexistent/claude"}}

	cases := []struct {
		name string
		call func() (string, error)
	}{
		{"summarize blank text", func() (string, error) { return s.Summarize(context.Background(), "  \n ") }},
		{"generate blank prompt", func() (string, error) { return g.GenerateText(context.Background(), "") }},
		{"translate blank text", func() (string, error) { return tr.Translate(context.Background(), " ", "French") }},
		{"translate blank language", func() (string, error) { return tr.Translate(context.Background(), "hello", " ") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.call()
			var se *domain.StructuredError
			if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
				t.Fatalf("err = %v, want INVALID_INPUT", err)
			}
		})
	}
}

func TestMissingBinaryReportsStructuredError(t *testing.T) {
	s := &Summarizer{cli: oneShot{bin: "definitely-not-a-real-binary-xyz"}}

	_, err := s.Summarize(context.Background(), "text")
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "CLAUDE_CLI_NOT_FOUND" {
		t.Fatalf("err = %v, want CLAUDE_CLI_NOT_FOUND", err)
	}
}

func TestCLIFailuresSurface(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"exit", "no auth token found"}, // stderr tail is kept in the error
		{"is_error", "error_max_turns"}, // exit 0, but the envelope says it failed
		{"empty", "empty result"},
		{"garbage", "decode result"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			bin, env := fakeOneShotCmd("CLAUDE_FAKE_MODE=" + tc.mode)
			s := &Summarizer{cli: oneShot{bin: bin, env: env}}

			_, err := s.Summarize(context.Background(), "text")
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCancelledContextStopsTheCLI(t *testing.T) {
	bin, env := fakeOneShotCmd()
	s := &Summarizer{cli: oneShot{bin: bin, env: env}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Summarize(ctx, "text")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
