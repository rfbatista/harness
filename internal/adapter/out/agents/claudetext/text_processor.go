package claudetext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Ensure each text-processing port has an adapter at compile time.
var (
	_ ports.Summarizer    = (*Summarizer)(nil)
	_ ports.TextGenerator = (*TextGenerator)(nil)
	_ ports.Translator    = (*Translator)(nil)
)

// oneShotMaxTurns caps the CLI at a single assistant turn. A text transform has
// no tools to call and nothing to iterate on, so a second turn could only mean
// the model started a conversation we never intend to continue.
const oneShotMaxTurns = 1

// System prompts replace the CLI's default coding-agent prompt entirely, which
// is what keeps the output a bare transformation instead of a chat reply.
const (
	// Deliberately says nothing about which language to write in: the model
	// keeps the input's language on its own, whereas naming the rule at all
	// ("write in the same language as the input") reliably made it translate
	// English input into the operator's own language instead.
	summarizeSystemPrompt = "You are a summarizer. Summarize the user's text faithfully and concisely. " +
		"Reply with the summary alone: no preamble, no closing remark, no markdown fences, " +
		"no commentary about the text or the task."

	generateSystemPrompt = "You are a text generator. Write exactly what the user's prompt asks for. " +
		"Reply with the requested text alone: no preamble, no closing remark, no markdown fences, " +
		"no commentary about the task."

	translateSystemPrompt = "You are a translator. Translate the user's text into %s, preserving its " +
		"meaning, tone, formatting and any markup. Do not answer, summarize or comment on the text — " +
		"translate it. Reply with the translation alone: no preamble, no closing remark, " +
		"no markdown fences, no notes."
)

// oneShot runs the claude CLI as a stateless text transformer: one prompt in on
// stdin, one answer out on stdout. It is the shared machinery behind the three
// text-processing adapters; each of them only supplies a system prompt.
//
// This is deliberately not a *Manager session. A session is a long-lived,
// tool-using, approval-gated process the user watches in the UI; a summary is a
// pure function of its input, and paying for a session's lifecycle, permission
// broker and event stream to get one would tie a text utility to the whole
// orchestration layer.
type oneShot struct {
	// bin is the claude CLI binary (Config.ClaudeBin).
	bin string
	// model is the CLI model alias or full name; empty leaves the CLI's own
	// configured default in place.
	model string
	// env replaces the child's environment when non-nil; nil inherits this
	// process's. Tests use it to drive a fake CLI.
	env []string
}

// args builds the argv for one transformation. Everything here exists to make
// the call a pure function: no tools, no MCP servers, no skills, no resumable
// session, and no project context.
func (o oneShot) args(systemPrompt string) []string {
	a := []string{
		"--print",
		"--output-format", "json",
		// A text transform must not touch the machine or reach the network
		// beyond the model API.
		"--tools", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
		// Nothing here is ever resumed, so it does not belong on disk.
		"--no-session-persistence",
		"--max-turns", strconv.Itoa(oneShotMaxTurns),
		// safe-mode drops CLAUDE.md, plugins, skills and hooks, so the same text
		// summarizes the same way regardless of which directory the server
		// happens to be running in.
		"--safe-mode",
		"--system-prompt", systemPrompt,
	}
	if o.model != "" {
		a = append(a, "--model", o.model)
	}
	return a
}

// run feeds input to the CLI and returns the transformed text.
func (o oneShot) run(ctx context.Context, systemPrompt, input string) (string, error) {
	cmd := exec.CommandContext(ctx, o.bin, o.args(systemPrompt)...)
	// The text travels on stdin, never in argv: it can be arbitrarily long
	// (ARG_MAX) and can start with "--", which as an argument the CLI would
	// parse as a flag.
	cmd.Stdin = strings.NewReader(input)
	// A neutral working directory: the CLI reports its cwd and git state to
	// itself, and a text transform should not vary with where the server runs.
	cmd.Dir = os.TempDir()
	if o.env != nil {
		cmd.Env = o.env
	}
	var stdout bytes.Buffer
	stderr := newStderrTail(nil)
	cmd.Stdout, cmd.Stderr = &stdout, stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", &domain.StructuredError{
				Code:    "CLAUDE_CLI_NOT_FOUND",
				Message: fmt.Sprintf("Claude CLI not found (%q). Install it with: npm install -g @anthropic-ai/claude-code", o.bin),
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("claude: %w: %s", err, stderr.String())
	}
	return decodeOneShotResult(stdout.Bytes(), stderr.String())
}

// oneShotResult is the `--output-format json` envelope: the whole run collapsed
// into a single result object.
type oneShotResult struct {
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// decodeOneShotResult turns the CLI's JSON envelope into the transformed text.
// A zero-exit run can still carry a failure (is_error with subtype
// "error_max_turns" or "error_during_execution"), so the exit code alone is not
// enough to call the call a success.
func decodeOneShotResult(stdout []byte, stderrTail string) (string, error) {
	var res oneShotResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &res); err != nil {
		return "", fmt.Errorf("claude: decode result: %w: %s", err, stderrTail)
	}
	if res.IsError {
		return "", fmt.Errorf("claude: %s: %s", res.Subtype, strings.TrimSpace(res.Result))
	}
	text := strings.TrimSpace(res.Result)
	if text == "" {
		return "", errors.New("claude: empty result")
	}
	return text, nil
}

// Summarizer condenses text with the Claude CLI.
type Summarizer struct{ cli oneShot }

// NewSummarizer returns the summarization adapter. model may be empty to keep
// the CLI's configured default.
func NewSummarizer(bin, model string) *Summarizer {
	return &Summarizer{cli: oneShot{bin: bin, model: model}}
}

func (s *Summarizer) Summarize(ctx context.Context, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "text is required"}
	}
	return s.cli.run(ctx, summarizeSystemPrompt, text)
}

// TextGenerator writes free-form text from a prompt with the Claude CLI.
type TextGenerator struct{ cli oneShot }

// NewTextGenerator returns the text-generation adapter. model may be empty to
// keep the CLI's configured default.
func NewTextGenerator(bin, model string) *TextGenerator {
	return &TextGenerator{cli: oneShot{bin: bin, model: model}}
}

func (g *TextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "prompt is required"}
	}
	return g.cli.run(ctx, generateSystemPrompt, prompt)
}

// Translator translates text with the Claude CLI.
type Translator struct{ cli oneShot }

// NewTranslator returns the translation adapter. model may be empty to keep the
// CLI's configured default.
func NewTranslator(bin, model string) *Translator {
	return &Translator{cli: oneShot{bin: bin, model: model}}
}

// Translate renders text in targetLanguage. The target language goes in the
// system prompt rather than alongside the text, so a document that itself talks
// about languages cannot redirect the instruction.
func (t *Translator) Translate(ctx context.Context, text, targetLanguage string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "text is required"}
	}
	if strings.TrimSpace(targetLanguage) == "" {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "targetLanguage is required"}
	}
	return t.cli.run(ctx, fmt.Sprintf(translateSystemPrompt, strings.TrimSpace(targetLanguage)), text)
}
