package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// ArtifactViewPath is where an artifact's bytes are served. The task tools
// hand it to the agent as view_url, so the application layer is given this
// function rather than importing the adapter to build the route.
func ArtifactViewPath(id string) string { return "/api/artifacts/" + url.PathEscape(id) + "/view/" }

// artifactCSP sandboxes agent-written HTML served from the harness origin:
// scripts may run, nothing may reach the API with the user's credentials.
// The web UI adds its own <iframe sandbox="allow-scripts">; both halves hold.
const artifactCSP = "sandbox allow-scripts; default-src 'self' data: blob:; img-src 'self' data: blob:; media-src 'self' data: blob:; " +
	"style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; font-src 'self' data:; connect-src 'none'; frame-ancestors 'self'"

func (h *Handler) handleListArtifacts(c echo.Context) error {
	if h.artifactsSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "artifacts not configured")
	}
	list, err := h.artifactsSvc.ListArtifacts(c.Request().Context(), ports.ArtifactFilter{
		SessionID: c.QueryParam("session_id"), TicketID: c.QueryParam("ticket_id"),
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"artifacts": list})
}

func (h *Handler) handleGetArtifact(c echo.Context) error {
	if h.artifactsSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "artifacts not configured")
	}
	a, err := h.artifactsSvc.GetArtifact(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, a)
}

func (h *Handler) handleDeleteArtifact(c echo.Context) error {
	if h.artifactsSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "artifacts not configured")
	}
	if err := h.artifactsSvc.DeleteArtifact(c.Request().Context(), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleArtifactViewRedirect(c echo.Context) error {
	return c.Redirect(http.StatusMovedPermanently, ArtifactViewPath(c.Param("id")))
}

// handleArtifactView streams the artifact's file, or a file next to it, from
// the session worktree. http.ServeContent supplies Content-Length, ranges
// (video seeks), If-None-Match against the ETag set here, and HEAD. The
// headers below are set first so ServeContent neither sniffs nor overrides.
func (h *Handler) handleArtifactView(c echo.Context) error {
	if h.artifactsSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "artifacts not configured")
	}
	f, err := h.artifactsSvc.OpenArtifactFile(c.Request().Context(), c.Param("id"), c.Param("*"))
	if err != nil {
		return err
	}
	defer f.Content.Close()

	ct := f.Mime
	if isTextMIME(ct) {
		ct += "; charset=utf-8"
	}
	hdr := c.Response().Header()
	hdr.Set("Content-Type", ct)
	hdr.Set("ETag", `"`+strconv.Itoa(f.Revision)+`"`)
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Disposition", "inline")
	hdr.Set("Content-Security-Policy", artifactCSP)
	http.ServeContent(c.Response(), c.Request(), "", f.ModTime, f.Content)
	return nil
}

// isTextMIME says which types get "; charset=utf-8": text, plus the
// text-shaped application types a page pulls in.
func isTextMIME(m string) bool {
	return strings.HasPrefix(m, "text/") || m == "application/json" || m == "application/javascript" || m == "image/svg+xml"
}
