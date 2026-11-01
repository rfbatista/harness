package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// designSystemBody is a project's design system as the API answers it.
type designSystemBody struct {
	ProjectID             string                    `json:"project_id"`
	Exists                bool                      `json:"exists"`
	Version               int                       `json:"version"`
	UpdatedAt             *time.Time                `json:"updated_at,omitempty"`
	UpdatedBy             string                    `json:"updated_by"`
	Record                domain.DesignSystemRecord `json:"record"`
	OpenProposals         int                       `json:"open_proposals"`
	RunningDesignSessions int                       `json:"running_design_sessions"`
}

// designVersionBody is one entry of a project's version list.
type designVersionBody struct {
	Version    int                  `json:"version"`
	At         time.Time            `json:"at"`
	By         string               `json:"by"`
	Source     string               `json:"source"`
	ProposalID string               `json:"proposal_id,omitempty"`
	Message    string               `json:"message"`
	Summary    domain.DesignSummary `json:"summary"`
}

func (h *Handler) designSystems() error {
	if h.designs == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "design systems are not configured")
	}
	return nil
}

func (h *Handler) handleGetDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ds, err := h.designs.GetDesignSystem(c.Request().Context(), in.ProjectID)
	if err != nil {
		return err
	}
	b := designSystemBody{ProjectID: ds.ProjectID, Exists: ds.Exists, Version: ds.Version, UpdatedBy: ds.UpdatedBy, Record: ds.Record}
	if ds.Exists {
		at := ds.UpdatedAt
		b.UpdatedAt = &at
	}
	return c.JSON(http.StatusOK, b)
}

func (h *Handler) handleSaveDesignSystem(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID   string                    `json:"project_id"`
		BaseVersion *int                      `json:"base_version"`
		Record      domain.DesignSystemRecord `json:"record"`
		Message     string                    `json:"message"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	v, err := h.designs.SaveDesignSystem(c.Request().Context(), in.ProjectID, *in.BaseVersion, in.Record, in.Message)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"version": v.Version})
}

func (h *Handler) handleRenameDesignToken(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID   string `json:"project_id"`
		BaseVersion *int   `json:"base_version"`
		Group       string `json:"group"`
		Name        string `json:"name"`
		NewGroup    string `json:"new_group"`
		NewName     string `json:"new_name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	v, err := h.designs.RenameDesignToken(c.Request().Context(), in.ProjectID, *in.BaseVersion, in.Group, in.Name, in.NewGroup, in.NewName)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int{"version": v.Version})
}

func (h *Handler) handleListDesignSystemVersions(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID string `json:"project_id"`
		Limit     int    `json:"limit"`
		Before    int    `json:"before"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	versions, err := h.designs.ListDesignSystemVersions(c.Request().Context(), in.ProjectID, in.Limit, in.Before)
	if err != nil {
		return err
	}
	out := make([]designVersionBody, 0, len(versions))
	for _, v := range versions {
		out = append(out, designVersionBody{Version: v.Version, At: v.At, By: v.By, Source: v.Source, ProposalID: v.ProposalID, Message: v.Message, Summary: v.Summary})
	}
	return c.JSON(http.StatusOK, map[string]any{"versions": out})
}

func (h *Handler) handleGetDesignSystemVersion(c echo.Context) error {
	if err := h.designSystems(); err != nil {
		return err
	}
	var in struct {
		ProjectID string `json:"project_id"`
		Version   int    `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	v, err := h.designs.GetDesignSystemVersion(c.Request().Context(), in.ProjectID, in.Version)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"version": v.Version, "at": v.At, "record": v.Record})
}
