package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

func (h *Handler) handleListBoundedContexts(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	return c.JSON(http.StatusOK, map[string]any{"bounded_contexts": h.svc.ListBoundedContexts(projectID)})
}

func (h *Handler) handleGetBoundedContext(c echo.Context) error {
	bc := h.svc.GetBoundedContext(c.QueryParam("bounded_context_id"))
	if bc == nil {
		return echo.NewHTTPError(http.StatusNotFound, "bounded context not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"bounded_context": bc})
}

func (h *Handler) handleCreateBoundedContext(c echo.Context) error {
	var in struct {
		ProjectID          string                `json:"project_id"`
		Name               string                `json:"name"`
		Purpose            string                `json:"purpose"`
		UbiquitousLanguage []domain.LanguageTerm `json:"ubiquitous_language"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	bc, err := h.svc.CreateBoundedContext(in.ProjectID, in.Name, in.Purpose, in.UbiquitousLanguage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"bounded_context": bc})
}

func (h *Handler) handleUpdateBoundedContext(c echo.Context) error {
	var in struct {
		BoundedContextID   string                `json:"bounded_context_id"`
		Name               string                `json:"name"`
		Purpose            string                `json:"purpose"`
		UbiquitousLanguage []domain.LanguageTerm `json:"ubiquitous_language"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	bc, err := h.svc.UpdateBoundedContext(in.BoundedContextID, in.Name, in.Purpose, in.UbiquitousLanguage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"bounded_context": bc})
}

func (h *Handler) handleDeleteBoundedContext(c echo.Context) error {
	var in struct {
		BoundedContextID string `json:"bounded_context_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.svc.DeleteBoundedContext(in.BoundedContextID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleAssignZoneToBoundedContext(c echo.Context) error {
	var in struct {
		ZoneID           string `json:"zone_id"`
		BoundedContextID string `json:"bounded_context_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	zone, err := h.svc.AssignZoneToBoundedContext(in.ZoneID, in.BoundedContextID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"zone": zone})
}

func (h *Handler) handleUnassignZoneFromBoundedContext(c echo.Context) error {
	var in struct {
		ZoneID string `json:"zone_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	zone, err := h.svc.UnassignZoneFromBoundedContext(in.ZoneID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"zone": zone})
}
