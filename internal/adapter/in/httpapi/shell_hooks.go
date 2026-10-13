package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// shellHookInput is the part of claude's hook input the edit proxy reads.
type shellHookInput struct {
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
}

// shellHook answers a claude hook's own input, posted by the commands
// orchestration.ShellHooks builds. A file tool's path is checked or
// committed as a write; a Bash command is checked for redirects into the
// shell ($HARNESS_SHELL and $HOME read from the X-Harness-Shell and
// X-Harness-Home headers). Allowed, the
// answer is empty; refused before the tool runs, it is a PreToolUse deny,
// and after, a block, each with the refusal's reason.
func (h *Handler) shellHook(c echo.Context, event string, answer func(ports.ShellEdit) ports.ShellAnswer) error {
	var in shellHookInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	e, ok := in.edit(event, c.Request().Header.Get("X-Harness-Shell"), c.Request().Header.Get("X-Harness-Home"))
	if !ok || h.shells == nil {
		return c.NoContent(http.StatusOK)
	}
	ans := answer(e)
	if ans.Allow {
		return c.NoContent(http.StatusOK)
	}
	var out any
	if event == "PreToolUse" {
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": ans.Reason,
		}}
	} else {
		out = map[string]any{"decision": "block", "reason": ans.Reason}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, b)
}

// edit is the shell edit the hook's tool call makes; false when it makes
// none the proxy looks at.
func (in shellHookInput) edit(event, shellDir, home string) (ports.ShellEdit, bool) {
	switch in.ToolName {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		p := in.ToolInput.FilePath
		if in.ToolName == "NotebookEdit" {
			p = in.ToolInput.NotebookPath
		}
		if p == "" {
			return ports.ShellEdit{}, false
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(in.Cwd, p)
		}
		return ports.ShellEdit{Op: "write", Paths: []string{p}, Tool: in.ToolName}, true
	case "Bash":
		if event != "PreToolUse" {
			return ports.ShellEdit{}, false
		}
		targets := redirectTargets(in.ToolInput.Command, in.Cwd, shellDir, home)
		if len(targets) == 0 {
			return ports.ShellEdit{}, false
		}
		return ports.ShellEdit{Op: "redirect", Paths: targets, Tool: "Bash"}, true
	}
	return ports.ShellEdit{}, false
}
