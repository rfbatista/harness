package app

import (
	"context"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/application/writinghelper"
)

// helperState is what every page reads of the writing helper: whether it can
// run, from the gate's state alone, so a page render never starts an install.
func helperState(h *writinghelper.Service) func(context.Context) shell.Helper {
	return func(ctx context.Context) shell.Helper {
		a := h.Availability(ctx)
		return shell.Helper{Available: a.Available, Reason: a.Reason, FixURL: a.FixURL}
	}
}
