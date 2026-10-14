// Package llm mounts the LLM proxy (spillway's llmproxy) on the harness's
// HTTP server at /llm/ (ADR 0006): every claude process the harness starts
// sets ANTHROPIC_BASE_URL to it, and each request it passes through reaches
// the harness's usage sink.
package llm

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/rfbatista/spillway/llmproxy"

	"operators-mcp/internal/application/usage"
)

// PathPrefix is where the LLM proxy is mounted; the claude CLI's base URL
// is the server's loopback address plus "/llm".
const PathPrefix = "/llm/"

// Recorder is the usage sink: the usage service satisfies it.
type Recorder interface {
	Record(ctx context.Context, r usage.Request)
}

// Config configures the mount.
type Config struct {
	// Upstream defaults to the Anthropic API.
	Upstream string
	Sink     Recorder
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// Handler is the LLM proxy as the server mounts it at PathPrefix: loopback
// peers only, the prefix stripped. It sits outside the /api token check,
// because the claude CLI cannot send the token.
func Handler(cfg Config) (http.Handler, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	proxy, err := llmproxy.New(llmproxy.Config{Upstream: cfg.Upstream, Sink: sink{cfg.Sink}, Logger: logger})
	if err != nil {
		return nil, err
	}
	return loopbackOnly(http.StripPrefix(strings.TrimSuffix(PathPrefix, "/"), proxy)), nil
}

// sink hands spillway's usage records to the harness's usage sink.
type sink struct{ r Recorder }

func (s sink) Record(ctx context.Context, r llmproxy.UsageRecord) {
	s.r.Record(ctx, usage.Request{
		ID: r.ID, Attribution: r.Attribution, Method: r.Method, Path: r.Path, Model: r.Model,
		Status: r.Status, Streamed: r.Streamed, StartedAt: r.StartedAt, EndedAt: r.EndedAt,
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		CacheCreationInputTokens: r.CacheCreationInputTokens, CacheReadInputTokens: r.CacheReadInputTokens,
		Error: r.Error,
	})
}

// loopbackOnly refuses every peer that is not this machine: the proxy
// carries the caller's credentials upstream, so nothing else may use it.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "the LLM proxy answers this machine only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
