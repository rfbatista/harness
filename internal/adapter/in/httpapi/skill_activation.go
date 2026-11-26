package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// SkillActivationRoute is where a session's PostToolUse hook asks which skill
// a touched file activates. Loopback-only, scoped by the session id.
const SkillActivationRoute = "/api/sessions/:id/skills/activate"

// SessionSkillsPath is the path the session's skill endpoints are under.
func SessionSkillsPath(sessionID string) string { return "/api/sessions/" + sessionID + "/skills" }

func (h *Handler) handleSkillActivation(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var in architectureHookInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	activator, ok := h.orchSvc.(ports.SkillActivator)
	if !ok {
		return c.NoContent(http.StatusOK)
	}
	p := in.ToolInput.FilePath
	if p == "" {
		p = in.ToolInput.NotebookPath
	}
	text := activator.ActivateSkillsOnTouch(c.Request().Context(), c.Param("id"), p)
	return hookAnswer(c, "PostToolUse", ports.ArchitectureVerdict{Context: text})
}
