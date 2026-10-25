package httpapi

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// architectureBody is a repository architecture as the API answers it.
type architectureBody struct {
	RepositoryID string                    `json:"repository_id"`
	Exists       bool                      `json:"exists"`
	Version      int                       `json:"version"`
	UpdatedAt    *time.Time                `json:"updated_at,omitempty"`
	Record       domain.ArchitectureRecord `json:"record"`
	Export       domain.ArchitectureExport `json:"export"`
}

func architectureBodyOf(a *domain.RepositoryArchitecture) architectureBody {
	b := architectureBody{RepositoryID: a.RepositoryID, Exists: a.Exists, Version: a.Version, Record: a.Record, Export: a.Export}
	if a.Exists {
		t := a.UpdatedAt
		b.UpdatedAt = &t
	}
	return b
}

func (h *Handler) repositoryArchitectures() error {
	if h.repoArch == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "repository architectures are not configured")
	}
	return nil
}

func (h *Handler) handleGetRepositoryArchitecture(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	a, err := h.repoArch.GetRepositoryArchitecture(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, architectureBodyOf(a))
}

func (h *Handler) handleSaveRepositoryArchitecture(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string                    `json:"repository_id"`
		BaseVersion  *int                      `json:"base_version"`
		Record       domain.ArchitectureRecord `json:"record"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	a, err := h.repoArch.SaveRepositoryArchitecture(c.Request().Context(), in.RepositoryID, *in.BaseVersion, in.Record)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, architectureBodyOf(a))
}

func (h *Handler) handleDeleteRepositoryArchitecture(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.repoArch.DeleteRepositoryArchitecture(c.Request().Context(), in.RepositoryID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// handleArchitectureSchema serves the JSON Schema of the architecture record.
func (h *Handler) handleArchitectureSchema(c echo.Context) error {
	return c.Blob(http.StatusOK, "application/schema+json", domain.ArchitectureSchemaJSON)
}
