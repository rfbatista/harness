package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// handleSessionMerge merges the session's branch into the target the person
// picked, with the method they picked, and answers how it ended (the new
// HEAD). An empty body is the default target with a merge commit. A refusal is
// a coded 409; it is on the session too.
func (h *Handler) handleSessionMerge(c echo.Context) error {
	if h.merges == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "merging is not configured")
	}
	var body struct {
		Target  string             `json:"target"`
		Method  domain.MergeMethod `json:"method"`
		Message string             `json:"message"`
	}
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	res, err := h.merges.MergeInto(c.Request().Context(), c.Param("id"),
		ports.MergeRequest{Target: body.Target, Method: body.Method, Message: body.Message})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

// handleSessionMergeOffer says whether the session's branch can be merged into
// ?target= (the default target when absent) and what the Merge tab shows.
func (h *Handler) handleSessionMergeOffer(c echo.Context) error {
	if h.merges == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "merging is not configured")
	}
	offer, err := h.merges.MergeOffer(c.Request().Context(), c.Param("id"), c.QueryParam("target"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, offer)
}
