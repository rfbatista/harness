package session

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
)

// approvalBar asks the user to allow or deny one tool call, oldest first.
type approvalBar struct{}

func (approvalBar) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "y", Desc: "approve"}, {Key: "n", Desc: "deny"}, {Key: "m", Desc: "deny with message"}, {Key: "tab", Desc: "to feed"}, {Key: "esc", Desc: "back"}}
}

func (approvalBar) key(m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	req, ok := m.pendingRequest()
	if !ok {
		return m, nil
	}
	if m.denyMessage != nil {
		return m.denyMessageKey(req, k)
	}
	if next, cmd, ok := m.controlKey(k); ok {
		return next, cmd
	}
	switch k.String() {
	case "tab":
		m.focusFeedAtTail()
	case "y":
		return m.decide(req, actionApprove, true, "")
	case "n":
		return m.decide(req, actionDeny, false, "")
	case "m":
		in := textinput.New()
		in.Prompt = "Deny with message: "
		in.Focus()
		m.denyMessage = &in
	}
	return m, nil
}

// denyMessageKey edits the optional message of a denial; enter sends it.
func (m Model) denyMessageKey(req Item, k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.denyMessage = nil
		return m, nil
	case "enter":
		msg := m.denyMessage.Value()
		m.deciding = true
		return m, m.action(actionDeny, func(ctx context.Context) error { return m.be.Resolve(ctx, m.id, req.ReqID, false, msg) })
	}
	in, cmd := m.denyMessage.Update(k)
	m.denyMessage = &in
	return m, cmd
}

// decide resolves the request once; further keys are ignored until the
// outcome comes back.
func (m Model) decide(req Item, kind actionKind, allow bool, message string) (Model, tea.Cmd) {
	if m.deciding {
		return m, nil
	}
	m.deciding = true
	return m, m.action(kind, func(ctx context.Context) error { return m.be.Resolve(ctx, m.id, req.ReqID, allow, message) })
}

func (approvalBar) view(m *Model, _ int) string {
	th := m.ctx.Theme
	req, _ := m.pendingRequest()
	title := lipgloss.NewStyle().Foreground(th.StatusColor(rollup.Review)).Bold(true).Render("Approve " + req.ToolName + "?")
	if n := len(m.tl.Pending); n > 1 {
		title += th.Meta().Render(fmt.Sprintf("  1 of %d", n))
	}
	body := th.Meta().Render(indent(prettyJSON(req.Input), "  "))
	if len(req.Input) == 0 {
		body = th.Meta().Render("  (no input)")
	}
	keys := th.Meta().Render("y approve  ·  n deny  ·  m deny with message")
	if m.deciding {
		keys = th.Meta().Render("Submitting…")
	}
	if m.denyMessage != nil {
		keys = m.denyMessage.View()
	}
	lines := []string{title, body, keys}
	if m.decideErr != "" {
		lines = append(lines, th.Error().Render(m.decideErr))
	}
	return strings.Join(lines, "\n")
}
