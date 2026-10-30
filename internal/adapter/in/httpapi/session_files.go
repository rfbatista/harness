package httpapi

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A session's virtual file system, read-only: its roots, a folder's entries a
// page at a time, and a file (contract "Harness server ↔ Web UI — a session's
// virtual file system"). Every answer and error code comes from
// ports.SessionFiles; the handlers only read the query.

func (h *Handler) sessionFilesOr503() error {
	if h.sessionFiles == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "session files are not available"}
	}
	return nil
}

func (h *Handler) handleSessionFiles(c echo.Context) error {
	if err := h.sessionFilesOr503(); err != nil {
		return err
	}
	roots, err := h.sessionFiles.SessionRoots(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, roots)
}

func (h *Handler) handleSessionFilesEntries(c echo.Context) error {
	if err := h.sessionFilesOr503(); err != nil {
		return err
	}
	q := ports.SessionEntriesQuery{Root: c.QueryParam("root"), Dir: c.QueryParam("dir"), After: c.QueryParam("after")}
	if raw := c.QueryParam("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be a positive integer"}
		}
		q.Limit = n
	}
	page, err := h.sessionFiles.SessionEntries(c.Request().Context(), c.Param("id"), q)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}

func (h *Handler) handleSessionFilesFile(c echo.Context) error {
	if err := h.sessionFilesOr503(); err != nil {
		return err
	}
	file, err := h.sessionFiles.SessionFile(c.Request().Context(), c.Param("id"), c.QueryParam("root"), c.QueryParam("path"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, file)
}
