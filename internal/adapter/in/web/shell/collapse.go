package shell

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// CollapsedCookie holds the rail groups the person folded, as comma-separated
// status keys ("backlog,done"). The browser writes it when a group is
// toggled; the shell reads it so the first paint is already folded. One
// preference per browser, for every project.
const CollapsedCookie = "rail_collapsed"

var statusKey = regexp.MustCompile(`^[a-z_]+$`)

// ParseCollapsed reads the cookie's value. A malformed value (anything but
// lower-case keys) collapses nothing; unknown keys are kept and match no group.
func ParseCollapsed(value string) map[string]bool {
	keys := map[string]bool{}
	for _, k := range strings.Split(value, ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !statusKey.MatchString(k) {
			return map[string]bool{}
		}
		keys[k] = true
	}
	return keys
}

type collapsedKey struct{}

// WithCollapsed carries the request's collapsed groups to the layout, which
// builds the rail without seeing the request.
func WithCollapsed(ctx context.Context, r *http.Request) context.Context {
	c, err := r.Cookie(CollapsedCookie)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, collapsedKey{}, ParseCollapsed(c.Value))
}

// CollapsedFrom is the collapsed groups WithCollapsed stored; empty when none.
func CollapsedFrom(ctx context.Context) map[string]bool {
	keys, _ := ctx.Value(collapsedKey{}).(map[string]bool)
	return keys
}
