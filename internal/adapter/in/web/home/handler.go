package home

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// BoardHref is the Agent board: the home page.
const BoardHref = "/"

// boardHints are the keys the stream bar names on the board.
var boardHints = []shell.Hint{
	{Keys: []string{"J", "K"}, Label: "move"},
	{Keys: []string{"Enter"}, Label: "open"},
	{Keys: []string{"/"}, Label: "filter"},
}

// Handler serves GET /.
type Handler struct {
	Projects ports.ProjectReader
	// Board is the Agent board's seed; nil keeps the old home page, which
	// opens the first project.
	Board  ports.AgentBoard
	Layout shell.Layout
	Render shell.Renderer
	Now    func() time.Time
}

// Index is the Agent board; with no project, it explains how to create one.
func (h Handler) Index(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if h.Board == nil {
		return h.openFirstProject(w, r)
	}
	// A page swapped in place carries no seed: the browser reads it behind a
	// skeleton that shows only when the read is slow.
	var seed *domain.BoardSeed
	if shell.ModeOf(ctx) == shell.ModeFull {
		var err error
		if seed, err = h.Board.Seed(ctx); err != nil {
			slog.Warn("agent board: seed not read; the browser reads it", "err", err)
			seed = nil
		}
	}
	if seed == nil {
		projects, err := h.Projects.ListProjects(ctx)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return h.welcome(w, r)
		}
	} else if seed.ProjectCount == 0 {
		return h.welcome(w, r)
	}
	frame, err := h.Layout(ctx, "Agent board", "", "")
	if err != nil {
		return err
	}
	frame.NoRail = true
	frame.Live = true // agentBoardPage follows the live session feed
	frame.Hints = boardHints
	frame.Top.OnBoard = true
	frame.Top.BoardHref = BoardHref
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	return h.Render(w, r, http.StatusOK, BoardPage(NewBoardView(frame, seed, now())))
}

func (h Handler) welcome(w http.ResponseWriter, r *http.Request) error {
	frame, err := h.Layout(r.Context(), "Welcome", "", "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, NoProjects(frame))
}

// openFirstProject is the home page of a server without an Agent board.
func (h Handler) openFirstProject(w http.ResponseWriter, r *http.Request) error {
	projects, err := h.Projects.ListProjects(r.Context())
	if err != nil {
		return err
	}
	if len(projects) > 0 {
		http.Redirect(w, r, "/projects/"+url.PathEscape(firstByName(projects).ID), http.StatusFound)
		return nil
	}
	return h.welcome(w, r)
}
