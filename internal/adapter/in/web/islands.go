package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/rfbatista/gohydrate"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/infra/pagetime"
	"operators-mcp/internal/ports"
)

// IslandRenderer renders a React island to an HTML fragment on the server:
// *gohydrate.Renderer is one. Deps.IslandRenderer replaces the one built from
// the embedded bundles.
type IslandRenderer interface {
	Render(ctx context.Context, name string, propsJSON []byte) (string, error)
}

// topBarIsland is the island's name in the manifest and in the page.
const topBarIsland = "top-bar"

// renderTimeout bounds one island render: past it the page carries an empty
// mount and the browser draws the top bar.
const renderTimeout = 200 * time.Millisecond

// newIslandRenderer builds the renderer from the embedded bundles, once at
// startup. It is nil when they are not built or cannot be loaded (logged): a
// page then carries an empty mount.
func (a *Assets) newIslandRenderer() IslandRenderer {
	if !a.IslandsBuilt() {
		return nil
	}
	r, err := gohydrate.NewRenderer(a.manifest, a.files, gohydrate.WithTimeout(renderTimeout))
	if err != nil {
		slog.Error("web: the island bundles cannot be loaded; pages carry an empty top bar for the browser to draw", "kind", islandErrorKind(err), "err", err)
		return nil
	}
	return r
}

// islandErrorKind names gohydrate's error kinds for the log.
func islandErrorKind(err error) string {
	switch {
	case errors.Is(err, gohydrate.ErrLoad):
		return "load"
	case errors.Is(err, gohydrate.ErrUnknownIsland):
		return "unknown_island"
	case errors.Is(err, gohydrate.ErrInvalidProps):
		return "invalid_props"
	case errors.Is(err, gohydrate.ErrRenderFailed):
		return "render_failed"
	case errors.Is(err, gohydrate.ErrTimeout):
		return "timeout"
	case errors.Is(err, gohydrate.ErrCancelled):
		return "cancelled"
	}
	return "unknown"
}

// island builds the top bar island of a page: the model as props and, when it
// renders, its HTML. The page never fails because of the island: on any
// failure the mount is empty and marked, and the error is logged.
func (s *server) island(ctx context.Context, bar *ports.TopBar) (*shell.TopBarIsland, error) {
	props, err := json.Marshal(bar)
	if err != nil {
		return nil, err
	}
	isl := &shell.TopBarIsland{Props: string(props), Failed: true}
	if s.renderer == nil {
		return isl, nil
	}
	started := time.Now()
	html, err := s.renderer.Render(ctx, topBarIsland, props)
	pagetime.AddIsland(ctx, time.Since(started))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("web: the top bar island did not render; the browser draws it", "kind", islandErrorKind(err), "path", bar.Path, "err", err)
		}
		return isl, nil
	}
	isl.HTML, isl.Failed = html, false
	return isl, nil
}
