package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Repository indexes: the codegraph index each repository keeps in its own
// checkout (ADR 0014). Both routes answer the port's types unchanged, so the
// Token savers page's seed and list_repository_indexes' answer are the same
// bytes.

var errRepositoryIndexesOff = echo.NewHTTPError(http.StatusServiceUnavailable, "repository indexes not configured")

// repositoryIndexesBody is list_repository_indexes' answer.
type repositoryIndexesBody struct {
	Indexes any `json:"repository_indexes"`
}

func (h *Handler) handleListRepositoryIndexes(c echo.Context) error {
	if h.repositoryIndexes == nil {
		return errRepositoryIndexesOff
	}
	list, err := h.repositoryIndexes.ListRepositoryIndexes(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, repositoryIndexesBody{Indexes: list})
}

// handleRebuildRepositoryIndex starts a build in the background and answers
// the index as it now stands; list_repository_indexes follows the build.
func (h *Handler) handleRebuildRepositoryIndex(c echo.Context) error {
	if h.repositoryIndexes == nil {
		return errRepositoryIndexesOff
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	idx, err := h.repositoryIndexes.RebuildRepositoryIndex(c.Request().Context(), in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repository_index": idx})
}
