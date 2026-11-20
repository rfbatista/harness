package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A project's own sign in with Claude (ADR 0102): the global flow's endpoints,
// taking a project_id and acting on that project's Claude home only. Every
// endpoint answers the flow's state or the login's status, never a token and
// never the code that was pasted.

// projectSignIn is the project's sign-in; the project must exist.
func (h *Handler) projectSignIn(c echo.Context, projectID string) (ports.AgentSignIn, error) {
	if h.projectAgentOAuth == nil {
		return nil, errAgentOAuthOff
	}
	if projectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	if h.projects != nil {
		if _, err := h.projects.GetProject(c.Request().Context(), projectID); err != nil {
			return nil, err
		}
	}
	return h.projectAgentOAuth.For(projectID), nil
}

func (h *Handler) handleGetProjectAgentOAuth(c echo.Context) error {
	flow, err := h.projectSignIn(c, c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, flow.State())
}

func (h *Handler) handleStartProjectAgentOAuth(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	flow, err := h.projectSignIn(c, in.ProjectID)
	if err != nil {
		return err
	}
	st, err := flow.Start()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleSubmitProjectAgentOAuthCode(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
		Code      string `json:"code"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	flow, err := h.projectSignIn(c, in.ProjectID)
	if err != nil {
		return err
	}
	st, err := flow.Submit(in.Code)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleCancelProjectAgentOAuth(c echo.Context) error {
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	flow, err := h.projectSignIn(c, in.ProjectID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, flow.Cancel())
}

func (h *Handler) handleGetProjectAgentAuthStatus(c echo.Context) error {
	flow, err := h.projectSignIn(c, c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, authAnswerOf(c, flow))
}
