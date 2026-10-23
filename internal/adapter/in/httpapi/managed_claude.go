package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// The managed claude (ADR 0019): installed versions, install, switch and the
// progress of an install. Every endpoint but the releases answers the whole
// state, which is what the Agent isolation page seeds and polls.

var errManagedClaudeOff = echo.NewHTTPError(http.StatusServiceUnavailable, "managed claude not configured")

type managedClaudeOut struct {
	ports.ManagedClaudeState
	// Overridden says -claude.bin / $CLAUDE_BIN decides which claude agents
	// run; Bin is then that path.
	Overridden bool   `json:"overridden"`
	Bin        string `json:"bin"`
}

func (h *Handler) managedClaudeOut(st ports.ManagedClaudeState) managedClaudeOut {
	out := managedClaudeOut{ManagedClaudeState: st, Bin: st.Path}
	if h.managedClaudeBin != nil {
		out.Overridden, out.Bin = h.managedClaudeBin.Overridden(), h.managedClaudeBin.Bin()
	}
	return out
}

func (h *Handler) handleGetManagedClaude(c echo.Context) error {
	if h.managedClaude == nil {
		return errManagedClaudeOff
	}
	return c.JSON(http.StatusOK, h.managedClaudeOut(h.managedClaude.State()))
}

func (h *Handler) handleListManagedClaudeReleases(c echo.Context) error {
	if h.managedClaude == nil {
		return errManagedClaudeOff
	}
	r, err := h.managedClaude.Releases(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "could not load versions: "+err.Error())
	}
	return c.JSON(http.StatusOK, r)
}

func (h *Handler) handleInstallManagedClaude(c echo.Context) error {
	return h.managedClaudeChange(c, h.managedClaude.Install)
}

func (h *Handler) handleSwitchManagedClaude(c echo.Context) error {
	return h.managedClaudeChange(c, func(_ context.Context, v string) (ports.ManagedClaudeState, error) {
		return h.managedClaude.Switch(v)
	})
}

func (h *Handler) managedClaudeChange(c echo.Context, change func(context.Context, string) (ports.ManagedClaudeState, error)) error {
	if h.managedClaude == nil {
		return errManagedClaudeOff
	}
	var in struct {
		Version string `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := change(c.Request().Context(), in.Version)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, h.managedClaudeOut(st))
}
