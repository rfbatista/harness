package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
)

// bindJSON decodes the request body as JSON into v. It is Content-Type-agnostic
// (unlike echo's c.Bind), matching the previous json.NewDecoder behavior so
// clients that omit the Content-Type header still work.
func bindJSON(c echo.Context, v any) error {
	if err := json.NewDecoder(c.Request().Body).Decode(v); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	return nil
}

// errorBody renders the {"error": msg} response contract, including the domain
// error code when there is one so clients can branch on it (e.g.
// confirm-and-retry on PUBLISH_TARGET_EXISTS) instead of matching message text.
func errorBody(message, code string) map[string]string {
	body := map[string]string{"error": message}
	if code != "" {
		body["code"] = code
	}
	return body
}

// errorHandler maps handler-returned errors to the {"error": msg} contract,
// preserving the status codes the previous writeDomainError produced.
func errorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	var se *domain.StructuredError
	if errors.As(err, &se) {
		switch se.Code {
		case "ZONE_NOT_FOUND", "PROJECT_NOT_FOUND", "AGENT_NOT_FOUND", "PROMPT_NOT_FOUND",
			"SKILL_NOT_FOUND", "MCP_SERVER_NOT_FOUND", "TOOL_NOT_FOUND", "TASK_NOT_FOUND",
			"TICKET_NOT_FOUND", "DOCUMENT_NOT_FOUND", "REPOSITORY_NOT_FOUND", "WORKSPACE_NOT_FOUND",
			"BOUNDED_CONTEXT_NOT_FOUND", "SESSION_NOT_FOUND", "SKILL_FILE_NOT_FOUND",
			"TERMINAL_NOT_FOUND", "ENV_FILE_NOT_FOUND",
			"RUN_NOT_FOUND", "RUN_COMMAND_NOT_FOUND", "ARTIFACT_NOT_FOUND":
			_ = c.JSON(http.StatusNotFound, errorBody(se.Message, se.Code))
			return
		case "INVALID_PATTERN", "INVALID_NAME", "INVALID_ROOT", "INVALID_PATH",
			"INVALID_INPUT", "INVALID_STATUS", "INVALID_URL",
			"CROSS_PROJECT_ACCESS", "AGENT_NOT_IN_ZONE", "NO_AGENTS", "MULTIPLE_AGENTS",
			"PUBLISH_ROOT_NOT_SET", "SESSION_HAS_NO_TASK", "DOCUMENT_NOT_ON_TASK", "DOCUMENT_NOT_HTML",
			"NO_ANSWERS", "ENV_FILE_TOO_LARGE",
			"ARTIFACT_PATH_OUTSIDE_WORKTREE", "ARTIFACT_URL_NOT_LOCAL", "ARTIFACT_TOO_LARGE", "ARTIFACT_KIND_MISMATCH",
			"ARTIFACT_NOT_PROMOTABLE":
			_ = c.JSON(http.StatusBadRequest, errorBody(se.Message, se.Code))
			return
		case "ARTIFACT_NOT_YOURS":
			_ = c.JSON(http.StatusForbidden, errorBody(se.Message, se.Code))
			return
		case "PUBLISH_TARGET_EXISTS", "PUBLISH_SLUG_CONFLICT", "WORKSPACE_EXISTS", "BRANCH_EXISTS",
			"SESSION_INTERACTIVE", "SESSION_NOT_INTERACTIVE", "SESSION_ALREADY_RUNNING",
			"SESSION_RUNS_ON_SERVER", "SESSION_RUNS_ON_TUI", "SESSION_NOT_RUNNING", "TASK_SESSION_LIMIT",
			// The session exists but its worktree or saved conversation is
			// gone: a state conflict, not a missing resource.
			"WORKSPACE_MISSING", "SESSION_TRANSCRIPT_MISSING",
			"ARTIFACT_IN_PROJECT":
			_ = c.JSON(http.StatusConflict, errorBody(se.Message, se.Code))
			return
		case "CLAUDE_CLI_NOT_FOUND", "AGENT_CLI_NOT_FOUND", "SERVER_HOSTING_UNAVAILABLE":
			// The request is well-formed; the machine just cannot run agents.
			_ = c.JSON(http.StatusServiceUnavailable, errorBody(se.Message, se.Code))
			return
		}
	}

	if se != nil && se.Code == "UNAUTHORIZED" {
		_ = c.JSON(http.StatusUnauthorized, errorBody(se.Message, se.Code))
		return
	}

	var he *echo.HTTPError
	if errors.As(err, &he) {
		msg := http.StatusText(he.Code)
		if m, ok := he.Message.(string); ok {
			msg = m
		}
		_ = c.JSON(he.Code, errorBody(msg, ""))
		return
	}

	_ = c.JSON(http.StatusInternalServerError, errorBody(err.Error(), ""))
}
