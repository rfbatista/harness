package claudetext

import (
	"context"

	"github.com/rfbatista/llmkit"
)

type projectKey struct{}

// WithProject says which project a one-shot or a turn is run for, so the
// confined drivers run it on that project's effective credential (ADR 0102).
// No project: the global agent credential.
func WithProject(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectKey{}, projectID)
}

// ProjectFrom is the project ctx names, "" for none.
func ProjectFrom(ctx context.Context) string {
	id, _ := ctx.Value(projectKey{}).(string)
	return id
}

// Prepared is what a confined call is prepared with: the env entries of its
// credential, the wrap that confines it, and the config dir it runs with when
// that is not the manager's (a project Claude home).
type Prepared struct {
	Env       []string
	Wrap      llmkit.WrapFunc
	ConfigDir string
}
