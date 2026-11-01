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

func (h *Handler) handlePreviewLayers(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string             `json:"repository_id"`
		Layers       []domain.ArchLayer `json:"layers"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.repoArch.PreviewLayers(c.Request().Context(), in.RepositoryID, in.Layers)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, p)
}

func (h *Handler) handleDiscoverRepositoryArchitecture(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	d, err := h.repoArch.DiscoverRepositoryArchitecture(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, d)
}

func (h *Handler) handleExportRepositoryArchitecture(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Version      int    `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	res, err := h.repoArch.ExportRepositoryArchitecture(c.Request().Context(), in.RepositoryID, in.Version)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// instructionFileBody is an instruction file as the API answers it: content
// only when the file is asked for.
type instructionFileBody struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Version   int       `json:"version"`
	Size      int       `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
	Content   *string   `json:"content,omitempty"`
}

func instructionFileBodyOf(f domain.InstructionFile, withContent bool) instructionFileBody {
	b := instructionFileBody{Path: f.Path, Kind: f.Kind, Version: f.Version, Size: f.Size, UpdatedAt: f.UpdatedAt}
	if withContent {
		b.Content = &f.Content
	}
	return b
}

func (h *Handler) handleListInstructionFiles(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	files, err := h.repoArch.ListInstructionFiles(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	out := make([]instructionFileBody, len(files))
	for i, f := range files {
		out[i] = instructionFileBodyOf(f, false)
	}
	copies, err := h.repoArch.ListRepositoryCopies(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	type copyBody struct {
		Path    string `json:"path"`
		Kind    string `json:"kind"`
		Differs bool   `json:"differs"`
	}
	cs := make([]copyBody, len(copies))
	for i, cp := range copies {
		cs[i] = copyBody{cp.Path, cp.Kind, cp.Differs}
	}
	return c.JSON(http.StatusOK, map[string]any{"files": out, "repository_copies": cs})
}

func (h *Handler) handleImportInstructionFiles(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string   `json:"repository_id"`
		Paths        []string `json:"paths"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	files, err := h.repoArch.ImportInstructionFiles(c.Request().Context(), in.RepositoryID, in.Paths)
	if err != nil {
		return err
	}
	out := make([]instructionFileBody, len(files))
	for i, f := range files {
		out[i] = instructionFileBodyOf(f, false)
	}
	return c.JSON(http.StatusOK, map[string]any{"files": out})
}

func (h *Handler) handleGetInstructionFile(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	f, err := h.repoArch.GetInstructionFile(c.Request().Context(), in.RepositoryID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, instructionFileBodyOf(*f, true))
}

func (h *Handler) handleSaveInstructionFile(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
		Kind         string `json:"kind"`
		BaseVersion  *int   `json:"base_version"`
		Content      string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	f, err := h.repoArch.SaveInstructionFile(c.Request().Context(), in.RepositoryID, in.Path, in.Kind, *in.BaseVersion, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, instructionFileBodyOf(*f, false))
}

func (h *Handler) handleRenameInstructionFile(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
		NewPath      string `json:"new_path"`
		BaseVersion  *int   `json:"base_version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	f, err := h.repoArch.RenameInstructionFile(c.Request().Context(), in.RepositoryID, in.Path, in.NewPath, *in.BaseVersion)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, instructionFileBodyOf(*f, false))
}

func (h *Handler) handleDeleteInstructionFile(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Path         string `json:"path"`
		BaseVersion  *int   `json:"base_version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.BaseVersion == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "base_version is required"}
	}
	if err := h.repoArch.DeleteInstructionFile(c.Request().Context(), in.RepositoryID, in.Path, *in.BaseVersion); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
