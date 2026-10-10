package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The code views: a repository at a ref or a session's worktree, read-only.
// Every answer and error code comes from ports.CodeBrowser; the handlers only
// read the query.

func (h *Handler) codeOr503() error {
	if h.code == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "code browsing is not available"}
	}
	return nil
}

func codeSource(c echo.Context) ports.CodeSourceQuery {
	return ports.CodeSourceQuery{
		RepositoryID: c.QueryParam("repository_id"),
		Ref:          c.QueryParam("ref"),
		WorkspaceID:  c.QueryParam("workspace_id"),
	}
}

func (h *Handler) handleCodeTree(c echo.Context) error {
	if err := h.codeOr503(); err != nil {
		return err
	}
	tree, err := h.code.CodeTree(c.Request().Context(), codeSource(c), c.QueryParam("path"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, tree)
}

func (h *Handler) handleCodePaths(c echo.Context) error {
	if err := h.codeOr503(); err != nil {
		return err
	}
	paths, err := h.code.CodePaths(c.Request().Context(), codeSource(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, paths)
}

func (h *Handler) handleCodeFile(c echo.Context) error {
	if err := h.codeOr503(); err != nil {
		return err
	}
	file, err := h.code.CodeFile(c.Request().Context(), codeSource(c), c.QueryParam("path"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, file)
}

func (h *Handler) handleCodeDiff(c echo.Context) error {
	if err := h.codeOr503(); err != nil {
		return err
	}
	diff, err := h.code.CodeDiff(c.Request().Context(), ports.CodeDiffQuery{
		WorkspaceID:  c.QueryParam("workspace_id"),
		Scope:        c.QueryParam("scope"),
		RepositoryID: c.QueryParam("repository_id"),
		Commit:       c.QueryParam("commit"),
		Base:         c.QueryParam("base"),
		Head:         c.QueryParam("head"),
		Path:         c.QueryParam("path"),
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, diff)
}

func (h *Handler) handleCodeMap(c echo.Context) error {
	if err := h.codeOr503(); err != nil {
		return err
	}
	m, err := h.code.CodeMap(c.Request().Context(), codeSource(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, m)
}
