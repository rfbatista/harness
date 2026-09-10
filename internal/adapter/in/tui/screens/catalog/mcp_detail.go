package catalog

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// mcpDetail shows one server's configuration, its last probe, and the tools
// the most recent probe in this session discovered.
type mcpDetail struct {
	ctx      core.Context
	be       backend.Backend
	snap     backend.Snapshot
	id       string
	probing  bool
	last     *domain.MCPProbeResult
	lastErr  error
	overlays components.Overlays
}

func newMCPDetail(ctx core.Context, be backend.Backend, snap backend.Snapshot, id string) *mcpDetail {
	return &mcpDetail{ctx: ctx, be: be, snap: snap, id: id}
}

func (d *mcpDetail) server() *domain.MCPServer {
	for _, s := range d.snap.MCPServers {
		if s.ID == d.id {
			return s
		}
	}
	return nil
}

func (d *mcpDetail) capturing() bool { return d.overlays.Active() }

func (d *mcpDetail) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "t", Desc: "test connection"}, {Key: "e", Desc: "edit"}}
}

func (d *mcpDetail) update(msg tea.Msg) (detail, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		d.ctx = msg.Ctx
		return d, nil
	case core.SnapshotMsg:
		d.snap = msg.Snapshot
		if d.server() == nil {
			return d, closeDetail
		}
		return d, nil
	case probeDoneMsg:
		d.probing = false
		d.lastErr = msg.err
		if msg.err == nil {
			res := msg.result
			d.last = &res
		}
		return d, func() tea.Msg { return core.RefreshMsg{} }
	}
	if cmd, handled := d.overlays.Update(msg); handled {
		return d, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		cmd, _ := d.overlays.Done(msg)
		return d, cmd
	case tea.KeyPressMsg:
		return d, d.key(msg)
	}
	return d, nil
}

func (d *mcpDetail) key(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		return closeDetail
	case "t":
		if !d.probing {
			d.probing = true
			return probe(d.be, d.id)
		}
	case "e":
		if s := d.server(); s != nil {
			d.overlays.OpenForm(mcpForm(d.ctx.Theme, "Edit MCP server", s), updateMCPServer(d.be, s.ID))
		}
	}
	return nil
}

func (d *mcpDetail) view() string {
	th := d.ctx.Theme
	s := d.server()
	if s == nil {
		return ""
	}
	label := th.Meta()
	var lines []string
	lines = append(lines, th.Title().Render(s.Name)+"  "+th.Subtitle().Render(s.Description), "")
	lines = append(lines, label.Render("transport  ")+s.Transport)
	if s.Command != "" {
		lines = append(lines, label.Render("command    ")+strings.TrimSpace(s.Command+" "+strings.Join(s.Args, " ")))
	}
	if s.URL != "" {
		lines = append(lines, label.Render("url        ")+s.URL)
	}
	if len(s.Env) > 0 {
		lines = append(lines, label.Render("env        ")+strings.ReplaceAll(formatEnv(s.Env), "\n", "\n           "))
	}
	lines = append(lines, "")
	switch {
	case d.probing:
		lines = append(lines, th.Subtitle().Render("Probing…"))
	case s.LastProbeAt == nil && d.last == nil:
		lines = append(lines, th.Subtitle().Render("never probed · press t to test the connection"))
	default:
		lines = append(lines, label.Render("last probe ")+probeCell(th, s.LastProbeStatus)+"  "+th.Meta().Render(fmt.Sprintf("%s · %s · %s", plural(s.ToolCount, "tool"), plural(s.ResourceCount, "resource"), plural(s.PromptCount, "prompt"))))
		if s.LastProbeError != "" {
			lines = append(lines, th.Error().Render(s.LastProbeError))
		}
	}
	if d.lastErr != nil {
		lines = append(lines, th.Error().Render("Probe failed: "+core.Message(d.lastErr)))
	}
	if d.last != nil {
		lines = append(lines, "")
		server := strings.TrimSpace(d.last.ServerName + " " + d.last.ServerVersion)
		if server != "" {
			lines = append(lines, label.Render("server     ")+server)
		}
		lines = append(lines, th.Title().Render(fmt.Sprintf("Discovered tools (%d)", len(d.last.ToolNames))))
		if len(d.last.ToolNames) == 0 {
			lines = append(lines, th.Subtitle().Render("none reported"))
		}
		for _, t := range d.last.ToolNames {
			lines = append(lines, "  • "+t)
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	if d.overlays.Active() {
		return components.OverlayOn(body, d.overlays.View(overlayWidth(d.ctx)), d.ctx.Width, d.ctx.Height)
	}
	return body
}
