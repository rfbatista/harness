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

// railIsland is the rail island's name in the manifest and in the page.
const railIsland = "rail"

// tasksToggleIsland is the header's Tasks button island's name in the manifest and in the page.
const tasksToggleIsland = "tasks-toggle"

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

// railIsland builds the rail island of a page, and the Tasks button's: the seed
// and navigation as props and, when they render, their HTML. Like the top bar,
// the page never fails because of them: on any failure the mount is empty and
// marked, and the browser draws it.
func (s *server) railIsland(ctx context.Context, frame shell.Frame) *shell.RailIsland {
	props, err := json.Marshal(frame.RailProps())
	if err != nil {
		props = []byte("{}")
	}
	toggleProps, _ := json.Marshal(shell.TasksToggleProps{NeedsYou: frame.Rail.Seed.NeedsYouCount()})
	isl := &shell.RailIsland{Props: string(props), Failed: true, ToggleProps: string(toggleProps), ToggleFailed: true}
	if s.renderer == nil {
		return isl
	}
	if html, ok := s.renderIsland(ctx, railIsland, props, frame.Top.Current); ok {
		isl.HTML, isl.Failed = html, false
	}
	if html, ok := s.renderIsland(ctx, tasksToggleIsland, toggleProps, frame.Top.Current); ok {
		isl.ToggleHTML, isl.ToggleFailed = html, false
	}
	return isl
}

func (s *server) renderIsland(ctx context.Context, name string, props []byte, project string) (string, bool) {
	started := time.Now()
	html, err := s.renderer.Render(ctx, name, props)
	pagetime.AddIsland(ctx, time.Since(started))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("web: an island did not render; the browser draws it", "island", name, "kind", islandErrorKind(err), "project", project, "err", err)
		}
		return "", false
	}
	return html, true
}
