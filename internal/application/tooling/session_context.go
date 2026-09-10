package tooling

import (
	"context"

	"github.com/rfbatista/harnesskit/tool"
)

// These forward to harnesskit/tool rather than defining a key of their own.
// The context key must exist in exactly one package: if this package declared
// its own, an inbound adapter setting the library's key would write a value
// SessionIDFrom could not read, and every session-scoped tool would silently
// see "" and fail closed on a request that was actually well-formed.

// WithSessionID tags a context with the session a tool call belongs to. Inbound
// adapters that serve a per-session tool surface set it from the request; the
// session-scoped tools read it back with SessionIDFrom instead of trusting an
// id supplied in the tool arguments.
func WithSessionID(ctx context.Context, id string) context.Context {
	return tool.WithSessionID(ctx, id)
}

// SessionIDFrom returns the session id tagged onto ctx, or "" when there is none.
func SessionIDFrom(ctx context.Context) string { return tool.SessionIDFrom(ctx) }
