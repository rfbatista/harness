package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// Token savers: installing, updating and removing them from the releases the
// harness pins (ADR 0012). Bodies are the port's types unchanged, so the
// Token savers page's seed and list_token_savers' answer are the same bytes.

var errTokenSaverInstallsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "token savers not configured")

func (h *Handler) handleListTokenSavers(c echo.Context) error {
	if h.tokenSaverInstalls == nil {
		return errTokenSaverInstallsOff
	}
	list, err := h.tokenSaverInstalls.ListTokenSavers(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, list)
}

// handleInstallTokenSaver installs, or updates to, the pinned version. The
// install runs in the background; list_token_savers follows it.
func (h *Handler) handleInstallTokenSaver(c echo.Context) error {
	if h.tokenSaverInstalls == nil {
		return errTokenSaverInstallsOff
	}
	var in struct {
		Saver domain.TokenSaver `json:"saver"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.tokenSaverInstalls.InstallTokenSaver(c.Request().Context(), in.Saver)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"token_saver": st})
}

func (h *Handler) handleRemoveTokenSaver(c echo.Context) error {
	if h.tokenSaverInstalls == nil {
		return errTokenSaverInstallsOff
	}
	var in struct {
		Saver domain.TokenSaver `json:"saver"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.tokenSaverInstalls.RemoveTokenSaver(c.Request().Context(), in.Saver)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"token_saver": st})
}
