package claudetext

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// TurnStreamer runs a resumable, read-only claude turn that streams its text
// and tool uses (llmkit's TurnStream and DeleteTranscript, which
// *claude.Manager implements): the architecture assistant's claude (ADR 0060).
type TurnStreamer interface {
	TurnStream(ctx context.Context, req claude.TurnRequest, on claude.TurnCallbacks) (claude.TurnResult, error)
	DeleteTranscript(sessionID string) error
}

// GatedTurn is Gated for a read-only turn: the same readiness check, before the
// CLI runs. Deleting a transcript is not gated: it starts nothing.
func GatedTurn(inner TurnStreamer, check func(ctx context.Context) error) TurnStreamer {
	if check == nil {
		return inner
	}
	return gatedTurn{inner: inner, check: check}
}

type gatedTurn struct {
	inner TurnStreamer
	check func(ctx context.Context) error
}

func (g gatedTurn) TurnStream(ctx context.Context, req claude.TurnRequest, on claude.TurnCallbacks) (claude.TurnResult, error) {
	if err := g.check(ctx); err != nil {
		return claude.TurnResult{}, err
	}
	return g.inner.TurnStream(ctx, req, on)
}

func (g gatedTurn) DeleteTranscript(id string) error { return g.inner.DeleteTranscript(id) }

// ConfinedTurn is Confined for a read-only turn: the agent credential and the
// sandbox wrap, prepared at each call for the turn's own directory, so the wrap
// can grant that one checkout read-only.
func ConfinedTurn(inner TurnStreamer, prepare func(dir string) ([]string, llmkit.WrapFunc, error)) TurnStreamer {
	if prepare == nil {
		return inner
	}
	return confinedTurn{inner: inner, prepare: prepare}
}

type confinedTurn struct {
	inner   TurnStreamer
	prepare func(dir string) ([]string, llmkit.WrapFunc, error)
}

func (c confinedTurn) TurnStream(ctx context.Context, req claude.TurnRequest, on claude.TurnCallbacks) (claude.TurnResult, error) {
	env, wrap, err := c.prepare(req.Dir)
	if err != nil {
		return claude.TurnResult{}, err
	}
	req.Env = append(slices.Clone(req.Env), env...)
	req.Wrap = wrap
	return c.inner.TurnStream(ctx, req, on)
}

func (c confinedTurn) DeleteTranscript(id string) error { return c.inner.DeleteTranscript(id) }

// ToolTarget is what a read-only tool use works on, from its raw JSON input: the
// file Read opens, or the pattern Grep and Glob search for, with the path they
// search in when they name one. "" when the tool is not one of them or the
// input is not what it should be.
func ToolTarget(tool string, input []byte) string {
	var in struct {
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		Path     string `json:"path"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	switch tool {
	case "Read":
		return in.FilePath
	case "Grep", "Glob":
		if in.Pattern != "" && in.Path != "" {
			return in.Pattern + " in " + in.Path
		}
		return in.Pattern
	}
	return ""
}

// RelativeTo shows a target relative to the checkout it names a path in; a path
// outside the checkout is left as it is.
func RelativeTo(checkout, target string) string {
	root := strings.TrimRight(checkout, "/")
	if root == "" {
		return target
	}
	target = strings.ReplaceAll(target, root+"/", "")
	return target
}
