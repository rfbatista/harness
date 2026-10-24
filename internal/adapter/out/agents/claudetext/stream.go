package claudetext

import (
	"context"
	"slices"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// StreamOneShotter runs one stateless claude turn like OneShotter and hands
// onText each piece of the answer as the CLI writes it: llmkit's
// OneShotStream, which *claude.Manager implements (ADR 0026).
type StreamOneShotter interface {
	OneShotStream(ctx context.Context, req claude.OneShotRequest, onText func(string)) (claude.OneShotResult, error)
}

// GatedStream is Gated for a streaming one-shot: the same readiness check,
// before the CLI runs.
func GatedStream(inner StreamOneShotter, check func(ctx context.Context) error) StreamOneShotter {
	if check == nil {
		return inner
	}
	return gatedStream{inner: inner, check: check}
}

type gatedStream struct {
	inner StreamOneShotter
	check func(ctx context.Context) error
}

func (g gatedStream) OneShotStream(ctx context.Context, req claude.OneShotRequest, onText func(string)) (claude.OneShotResult, error) {
	if err := g.check(ctx); err != nil {
		return claude.OneShotResult{}, err
	}
	return g.inner.OneShotStream(ctx, req, onText)
}

// ConfinedStream is Confined for a streaming one-shot: the same agent
// credential and sandbox wrap, prepared at each call.
func ConfinedStream(inner StreamOneShotter, prepare func() ([]string, llmkit.WrapFunc, error)) StreamOneShotter {
	if prepare == nil {
		return inner
	}
	return confinedStream{inner: inner, prepare: prepare}
}

type confinedStream struct {
	inner   StreamOneShotter
	prepare func() ([]string, llmkit.WrapFunc, error)
}

func (c confinedStream) OneShotStream(ctx context.Context, req claude.OneShotRequest, onText func(string)) (claude.OneShotResult, error) {
	env, wrap, err := c.prepare()
	if err != nil {
		return claude.OneShotResult{}, err
	}
	req.Env = append(slices.Clone(req.Env), env...)
	req.Wrap = wrap
	return c.inner.OneShotStream(ctx, req, onText)
}
