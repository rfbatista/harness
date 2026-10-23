package claudetext

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Ensure each text-processing port has an adapter at compile time.
var (
	_ ports.Summarizer    = (*Summarizer)(nil)
	_ ports.TextGenerator = (*TextGenerator)(nil)
	_ ports.Translator    = (*Translator)(nil)
)

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

// OneShotter runs one stateless claude turn: llmkit's one-shot mode, which
// *claude.Manager implements, routed through the LLM proxy.
type OneShotter interface {
	OneShot(ctx context.Context, req claude.OneShotRequest) (claude.OneShotResult, error)
}

// oneShot is the shared machinery behind the three text-processing
// adapters; each of them only supplies a system prompt. llmkit owns the CLI's
// arguments: no tools, no MCP, no skills, no session persistence, a neutral
// working directory, the input on stdin.
type oneShot struct {
	claude OneShotter
	// bin names the CLI in the error a missing one gives.
	bin string
	// model is the CLI model alias or full name; empty leaves the CLI's own
	// configured default in place.
	model string
}

// run sends input under systemPrompt and returns the answer.
func (o oneShot) run(ctx context.Context, systemPrompt, input string) (string, error) {
	res, err := o.claude.OneShot(ctx, claude.OneShotRequest{System: systemPrompt, Input: input, Model: o.model, Attribution: ports.UsageSystemAttribution})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", &domain.StructuredError{
				Code:    "CLAUDE_CLI_NOT_FOUND",
				Message: fmt.Sprintf("Claude CLI not found (%q). Install it with: npm install -g @anthropic-ai/claude-code", o.bin),
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	return res.Text, nil
}

// Summarizer condenses text with the Claude CLI.
type Summarizer struct{ cli oneShot }

// NewSummarizer returns the summarization adapter. model may be empty to keep
// the CLI's configured default.
func NewSummarizer(c OneShotter, bin, model string) *Summarizer {
	return &Summarizer{cli: oneShot{claude: c, bin: bin, model: model}}
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
func NewTextGenerator(c OneShotter, bin, model string) *TextGenerator {
	return &TextGenerator{cli: oneShot{claude: c, bin: bin, model: model}}
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
func NewTranslator(c OneShotter, bin, model string) *Translator {
	return &Translator{cli: oneShot{claude: c, bin: bin, model: model}}
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

// Gated refuses every one-shot with what check answers, before the CLI runs:
// a one-shot is an agent process like a session, so it needs the same agent
// credential, managed claude and sandbox driver. A nil check lets all through.
func Gated(inner OneShotter, check func(ctx context.Context) error) OneShotter {
	if check == nil {
		return inner
	}
	return gated{inner: inner, check: check}
}

type gated struct {
	inner OneShotter
	check func(ctx context.Context) error
}

func (g gated) OneShot(ctx context.Context, req claude.OneShotRequest) (claude.OneShotResult, error) {
	if err := g.check(ctx); err != nil {
		return claude.OneShotResult{}, err
	}
	return g.inner.OneShot(ctx, req)
}

// Confined prepares every one-shot for the agent isolation: prepare is asked
// at each call for the env entries of the agent credential and the wrap that
// runs the CLI under the sandbox (nil with the sandbox off). Its error refuses
// the call before the CLI runs. A nil prepare leaves the request as it is.
func Confined(inner OneShotter, prepare func() ([]string, llmkit.WrapFunc, error)) OneShotter {
	if prepare == nil {
		return inner
	}
	return confined{inner: inner, prepare: prepare}
}

type confined struct {
	inner   OneShotter
	prepare func() ([]string, llmkit.WrapFunc, error)
}

func (c confined) OneShot(ctx context.Context, req claude.OneShotRequest) (claude.OneShotResult, error) {
	env, wrap, err := c.prepare()
	if err != nil {
		return claude.OneShotResult{}, err
	}
	req.Env = append(slices.Clone(req.Env), env...)
	req.Wrap = wrap
	return c.inner.OneShot(ctx, req)
}
