package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// designImports is the import the design systems offer, nil when they offer none.
func designImports(d ports.DesignSystems) ports.DesignSystemImports {
	imp, _ := d.(ports.DesignSystemImports)
	return imp
}

func (h *Handler) designImports() error {
	if h.imports == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "design system import is not configured")
	}
	return nil
}

type designSourceBody struct {
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
	DesignMD     string `json:"design_md"`
	Tokens       string `json:"tokens"`
}

func (h *Handler) handleFindDesignSystemSources(c echo.Context) error {
	if err := h.designImports(); err != nil {
		return err
	}
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	sources, err := h.imports.FindDesignSystemSources(c.Request().Context(), in.ProjectID)
	if err != nil {
		return err
	}
	out := make([]designSourceBody, 0, len(sources))
	for _, s := range sources {
		out = append(out, designSourceBody{RepositoryID: s.RepositoryID, Name: s.Name, DesignMD: s.DesignMD, Tokens: s.Tokens})
	}
	return c.JSON(http.StatusOK, map[string]any{"repositories": out})
}

// designImportIn is the request both the preview and the import take.
type designImportIn struct {
	ProjectID     string `json:"project_id"`
	RepositoryID  string `json:"repository_id"`
	DesignMDPath  string `json:"design_md_path"`
	TokensPath    string `json:"tokens_path"`
	ComponentsDir string `json:"components_dir"`
}

func (in designImportIn) request() ports.DesignImportRequest {
	return ports.DesignImportRequest{
		ProjectID: in.ProjectID, RepositoryID: in.RepositoryID,
		DesignMDPath: in.DesignMDPath, TokensPath: in.TokensPath, ComponentsDir: in.ComponentsDir,
	}
}

func (h *Handler) handlePreviewDesignSystemImport(c echo.Context) error {
	if err := h.designImports(); err != nil {
		return err
	}
	var in designImportIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	pv, err := h.imports.PreviewDesignSystemImport(c.Request().Context(), in.request())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"found":   map[string]any{"design_md": pv.FoundDesignMD, "tokens": pv.FoundTokens, "components": pv.FoundComponents},
		"record":  pv.Record,
		"counts":  pv.Counts,
		"skipped": pv.Skipped,
	})
}

func (h *Handler) handleImportDesignSystem(c echo.Context) error {
	if err := h.designImports(); err != nil {
		return err
	}
	var in struct {
		designImportIn
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	id, v, err := h.imports.ImportDesignSystem(c.Request().Context(), in.request(), in.Name, in.Slug, in.Description)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"design_system_id": id.ID, "version": v.Version})
}

// designVersionRepository is the repository an imported version read.
type designVersionRepository struct {
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
}

func versionRepository(v *domain.DesignSystemVersion) *designVersionRepository {
	if v.RepositoryID == "" {
		return nil
	}
	return &designVersionRepository{RepositoryID: v.RepositoryID, Name: v.RepositoryName}
}
