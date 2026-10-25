package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architecture rules' endpoints: a session's hooks, PATH shims and git
// pre-commit hook ask them before they let an action go on (contract
// "Harness server ↔ Agent sessions — repository architecture record"). Like
// the shell endpoints they are loopback-only and need no token: the session id
// in the path scopes them.
//
// check answers claude's own hook input (?hook=PreToolUse or PostToolUse) in
// claude's hook output format, or a shim's {op, paths, tool} with a verdict.
// commit answers a commit check's {paths} with a verdict.
const (
	ArchitectureCheckRoute  = "/api/sessions/:id/architecture/check"
	ArchitectureCommitRoute = "/api/sessions/:id/architecture/commit"
)

// SessionArchitecturePath is the path the session's architecture endpoints
// are under: …/check and …/commit.
func SessionArchitecturePath(sessionID string) string {
	return "/api/sessions/" + sessionID + "/architecture"
}

// architectureHookInput is the part of claude's hook input the rule check
// reads.
type architectureHookInput struct {
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
	} `json:"tool_input"`
}

func (h *Handler) handleArchitectureCheck(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	sessionID := c.Param("id")
	ctx := c.Request().Context()
	if event := c.QueryParam("hook"); event != "" {
		if event != "PreToolUse" && event != "PostToolUse" {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "hook must be PreToolUse or PostToolUse"}
		}
		var in architectureHookInput
		if err := bindJSON(c, &in); err != nil {
			return err
		}
		if h.archRules == nil {
			return c.NoContent(http.StatusOK)
		}
		check := ports.ArchitectureCheck{SessionID: sessionID, Tool: in.ToolName, Point: domain.PointBeforeTool, Command: in.ToolInput.Command}
		if event == "PostToolUse" {
			check.Point = domain.PointAfterEdit
		}
		if p := in.ToolInput.FilePath; p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(in.Cwd, p)
			}
			check.Paths = []string{p}
		}
		return hookAnswer(c, event, h.archRules.CheckArchitecture(ctx, check))
	}
	var in struct {
		Op    string   `json:"op"`
		Paths []string `json:"paths"`
		Tool  string   `json:"tool"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if h.archRules == nil {
		return c.JSON(http.StatusOK, ports.ArchitectureVerdict{})
	}
	return c.JSON(http.StatusOK, h.archRules.CheckArchitecture(ctx, ports.ArchitectureCheck{
		SessionID: sessionID, Point: domain.PointBeforeTool, Tool: in.Tool, Paths: in.Paths,
	}))
}

func (h *Handler) handleArchitectureCommit(c echo.Context) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	var in struct {
		Paths []string `json:"paths"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if h.archRules == nil {
		return c.JSON(http.StatusOK, ports.ArchitectureVerdict{})
	}
	return c.JSON(http.StatusOK, h.archRules.CheckArchitecture(c.Request().Context(), ports.ArchitectureCheck{
		SessionID: c.Param("id"), Point: domain.PointBeforeCommit, Tool: "git commit", Paths: in.Paths,
	}))
}

// hookAnswer renders a verdict in claude's hook output format: nothing when
// every rule passed; before a tool, a deny or the warnings as context; after
// an edit, the text as context, or a block's reason in a PostToolUse.
func hookAnswer(c echo.Context, event string, v ports.ArchitectureVerdict) error {
	var out map[string]any
	switch {
	case v.Deny == "" && v.Context == "":
		return c.NoContent(http.StatusOK)
	case v.Deny != "" && event == "PreToolUse":
		out = map[string]any{"hookEventName": event, "permissionDecision": "deny", "permissionDecisionReason": v.Deny}
	default:
		text := v.Context
		if v.Deny != "" {
			text = v.Deny
			if v.Context != "" {
				text += "\n" + v.Context
			}
		}
		out = map[string]any{"hookEventName": event, "additionalContext": text}
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": out})
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, b)
}

func (h *Handler) handleListArchitectureRuleEvents(c echo.Context) error {
	if err := h.repositoryArchitectures(); err != nil {
		return err
	}
	var in struct {
		RepositoryID string `json:"repository_id"`
		Since        string `json:"since"`
		Limit        int    `json:"limit"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	var since time.Time
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339Nano, in.Since)
		if err != nil {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "since must be an RFC 3339 time"}
		}
		since = t
	}
	events, err := h.repoArch.ListArchitectureRuleEvents(c.Request().Context(), in.RepositoryID, since, in.Limit)
	if err != nil {
		return err
	}
	if events == nil {
		events = []domain.ArchitectureRuleEvent{}
	}
	return c.JSON(http.StatusOK, map[string]any{"events": events})
}
