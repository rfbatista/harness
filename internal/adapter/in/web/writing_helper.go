package web

import (
	"context"
	"sync"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// helperCacheTTL is how long a page render may reuse the writing helper's
// readiness. Every page asks, and the check can start an install of the
// managed claude, so it is not run per page.
const helperCacheTTL = 3 * time.Second

// helperCache keeps the writing helper's readiness for helperCacheTTL.
type helperCache struct {
	helper ports.WritingHelperReadiness
	now    func() time.Time

	mu   sync.Mutex
	at   time.Time
	last ports.WritingHelperAvailability
}

func newHelperCache(h ports.WritingHelperReadiness, now func() time.Time) *helperCache {
	if now == nil {
		now = time.Now
	}
	return &helperCache{helper: h, now: now}
}

// availability is the readiness, read at most once per TTL. Without a helper
// it is unavailable, with no fix to point at.
func (c *helperCache) availability(ctx context.Context) ports.WritingHelperAvailability {
	if c == nil || c.helper == nil {
		return ports.WritingHelperAvailability{Reason: "the writing helper is not served by this server"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t := c.now(); c.at.IsZero() || t.Sub(c.at) >= helperCacheTTL {
		c.last, c.at = c.helper.Availability(ctx), t
	}
	return c.last
}

// get is the frame's view of the readiness.
func (c *helperCache) get(ctx context.Context) shell.WritingHelper {
	a := c.availability(ctx)
	return shell.WritingHelper{Available: a.Available, Reason: a.Reason, FixURL: a.FixURL}
}
