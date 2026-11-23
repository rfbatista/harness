package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
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
	// WorktreeArchitectureCommitRoute is where a commit made with no session
	// environment is checked: the worktree's own endpoint, which attributes it
	// to a session on the worktree (ADR 0105).
	WorktreeArchitectureCommitRoute = "/api/worktrees/:id/architecture/commit"
)

// WorktreeArchitecturePath is the path the worktree's commit endpoint is
// under: …/commit.
func WorktreeArchitecturePath(workspaceID string) string {
	return "/api/worktrees/" + workspaceID + "/architecture"
}

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
		// NotebookPath is the notebook NotebookEdit writes.
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
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
		p := in.ToolInput.FilePath
		if p == "" {
			p = in.ToolInput.NotebookPath
		}
		if p != "" {
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
		SessionID: sessionID, Point: domain.PointBeforeTool, Tool: in.Tool, Paths: in.Paths, Shim: true,
	}))
}

// handleArchitectureCommit answers a session's pre-commit hook: the body is the
// staged paths, NUL separated, and the answer is text for git to print: 409
// when a blocking rule fails the commit, 200 with the warnings (or nothing).
func (h *Handler) handleArchitectureCommit(c echo.Context) error {
	return h.checkCommit(c, c.Param("id"))
}

// handleWorktreeArchitectureCommit answers a pre-commit hook that ran with no
// session environment: the commit is checked as the session the worktree
// attributes it to, and the answer names that session in X-Harness-Attributed-To.
func (h *Handler) handleWorktreeArchitectureCommit(c echo.Context) error {
	// Checked here as well as in checkCommit: the attribution is looked up, and
	// named in a header, before checkCommit runs.
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	who, ok := h.orchSvc.(ports.CommitAttributor)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "commit attribution is not available")
	}
	attributed, err := who.CommitAttribution(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	if attributed.SessionID == "" {
		c.Response().Header().Set("X-Harness-Attributed-To", "worktree")
		return h.checkCommitAs(c, ports.ArchitectureCheck{Worktree: &ports.CheckedWorktree{
			RepositoryID: attributed.RepositoryID, ProjectID: attributed.ProjectID, Dir: attributed.Dir,
		}})
	}
	c.Response().Header().Set("X-Harness-Attributed-To", attributed.SessionID)
	return h.checkCommit(c, attributed.SessionID)
}

// checkCommit runs the commit check for sessionID on the staged paths in the
// body.
func (h *Handler) checkCommit(c echo.Context, sessionID string) error {
	return h.checkCommitAs(c, ports.ArchitectureCheck{SessionID: sessionID})
}

// checkCommitAs runs the commit check described by check, whose commit fields
// it fills from the staged paths in the body.
func (h *Handler) checkCommitAs(c echo.Context, check ports.ArchitectureCheck) error {
	if !isLoopback(c.Request().RemoteAddr) {
		return echo.NewHTTPError(http.StatusForbidden, "loopback only")
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request().Body, 8<<20))
	if err != nil {
		return err
	}
	var paths []string
	for _, p := range strings.Split(string(raw), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if h.archRules == nil {
		return c.NoContent(http.StatusOK)
	}
	check.Point, check.Tool, check.Paths = domain.PointBeforeCommit, "git commit", paths
	v := h.archRules.CheckArchitecture(c.Request().Context(), check)
	if v.Deny != "" {
		text := v.Deny
		if v.Context != "" {
			text += "\n" + v.Context
		}
		return c.String(http.StatusConflict, text+"\n")
	}
	if v.Context == "" {
		return c.NoContent(http.StatusOK)
	}
	return c.String(http.StatusOK, v.Context+"\n")
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
