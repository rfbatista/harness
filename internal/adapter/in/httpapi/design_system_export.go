package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// designExports is the export the design systems offer, nil when they offer none.
func designExports(d ports.DesignSystems) ports.DesignSystemExports {
	exp, _ := d.(ports.DesignSystemExports)
	return exp
}

func (h *Handler) exportsReady() error {
	if h.exports == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "design system export is not configured")
	}
	return nil
}

func (h *Handler) handlePreviewDesignSystemExport(c echo.Context) error {
	if err := h.exportsReady(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	files, err := h.exports.PreviewDesignSystemExport(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"files": files})
}

func (h *Handler) handleExportDesignSystem(c echo.Context) error {
	if err := h.exportsReady(); err != nil {
		return err
	}
	var in struct {
		RepositoryID         string `json:"repository_id"`
		ConfirmEditedOutside bool   `json:"confirm_edited_outside"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	res, err := h.exports.ExportDesignSystem(c.Request().Context(), in.RepositoryID, in.ConfirmEditedOutside)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}
