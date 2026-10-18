package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// Token saver activations: each saver's setting at project, repository or
// task level, answered with what it resolves to. Both routes answer the
// owner's activations: {"level", "owner_id", "activations": [...]}.

func (h *Handler) handleListTokenSaverActivations(c echo.Context) error {
	if h.tokenSavers == nil {
		return errTokenSaversOff
	}
	owner := domain.ActivationOwner{Level: domain.ActivationLevel(c.QueryParam("level")), ID: c.QueryParam("owner_id")}
	list, err := h.tokenSavers.ListTokenSaverActivations(c.Request().Context(), owner)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, activationsBody(owner, list))
}

func (h *Handler) handleSetTokenSaverActivation(c echo.Context) error {
	if h.tokenSavers == nil {
		return errTokenSaversOff
	}
	var in struct {
		Level   string `json:"level"`
		OwnerID string `json:"owner_id"`
		Saver   string `json:"saver"`
		Value   string `json:"value"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	owner := domain.ActivationOwner{Level: domain.ActivationLevel(in.Level), ID: in.OwnerID}
	list, err := h.tokenSavers.SetTokenSaverActivation(c.Request().Context(), owner, domain.TokenSaver(in.Saver), domain.ActivationValue(in.Value))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, activationsBody(owner, list))
}

func activationsBody(owner domain.ActivationOwner, list []domain.TokenSaverActivation) map[string]any {
	return map[string]any{"level": owner.Level, "owner_id": owner.ID, "activations": list}
}
