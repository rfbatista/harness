package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// handleSessionMerge merges the session's branch with --no-ff into its
// repository's default branch in the local checkout, and answers how it ended
// (the new HEAD). A refusal is a coded 409; it is on the session too.
func (h *Handler) handleSessionMerge(c echo.Context) error {
	if h.merges == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "merging is not configured")
	}
	res, err := h.merges.MergeSessionBranch(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// handleSessionMergeOffer says whether a merge applies to the session and
// into which branch it would go.
func (h *Handler) handleSessionMergeOffer(c echo.Context) error {
	if h.merges == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "merging is not configured")
	}
	offer, err := h.merges.MergeOffer(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, offer)
}
