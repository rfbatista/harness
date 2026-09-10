package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

// mcpsSpec lists MCP servers: n/e configure, t probes, i imports JSON, Enter
// opens the detail with discovered tools.
type mcpsSpec struct{}

// probeDoneMsg reports a probe; the list toasts it, the detail shows the tools.
type probeDoneMsg struct {
	id     string
	result domain.MCPProbeResult
	err    error
}

const probeTimeout = 30 * time.Second

func probe(be backend.Backend, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		res, err := be.ProbeMCPServer(ctx, id)
		return probeDoneMsg{id: id, result: res, err: err}
	}
}

func (mcpsSpec) columns() []components.Column {
	return []components.Column{{Title: "Server", Width: 24}, {Title: "Transport", Width: 16}, {Title: "Probe", Width: 10}, {Title: "Tools", Width: 10}, {Title: "Agents", Width: 8}}
}

func (mcpsSpec) entries(m *Model) []entry {
	th := m.ctx.Theme
	var out []entry
	for _, s := range m.snap.MCPServers {
		out = append(out, entry{s.ID, []string{s.Name, s.Transport, probeCell(th, s.LastProbeStatus), plural(s.ToolCount, "tool"), fmt.Sprintf("%d", s.AgentCount)}})
	}
	return out
}

func probeCell(th theme.Theme, status string) string {
	switch status {
	case "":
		return th.Meta().Render("unknown")
	case domain.MCPProbeStatusOK:
		return lipgloss.NewStyle().Foreground(th.Accent).Render(status)
	case domain.MCPProbeStatusUnknown:
		return th.Meta().Render(status)
	}
	return th.Error().Render(status)
}

func (mcpsSpec) summary() string   { return "MCP server catalog" }
func (mcpsSpec) emptyText() string { return "No MCP servers yet. Press n to add one or i to import." }

func (mcpsSpec) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "↵", Desc: "open"}, {Key: "n", Desc: "new"}, {Key: "e", Desc: "edit"}, {Key: "t", Desc: "test"}, {Key: "i", Desc: "import"}, {Key: "D", Desc: "delete"}}
}

func (mcpsSpec) server(m *Model, id string) *domain.MCPServer {
	for _, s := range m.snap.MCPServers {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (s mcpsSpec) key(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	th, be := m.ctx.Theme, m.be
	switch k.String() {
	case "n":
		m.overlays.OpenForm(mcpForm(th, "New MCP server", nil), func(sub components.FormSubmitMsg) tea.Cmd {
			in := mcpInput(sub.Values)
			return components.Op("mcp.create", "MCP server added", func() error {
				_, err := be.CreateMCPServer(in)
				return err
			})
		})
		return nil, true
	case "e":
		if srv := s.server(m, m.Selected()); srv != nil {
			m.overlays.OpenForm(mcpForm(th, "Edit MCP server", srv), updateMCPServer(be, srv.ID))
		}
		return nil, true
	case "t":
		if srv := s.server(m, m.Selected()); srv != nil {
			return probe(m.be, srv.ID), true
		}
		return nil, true
	case "i":
		m.overlays.OpenForm(components.NewForm(th, "Import MCP servers",
			components.MultilineField("content", "JSON with a top-level mcpServers object", "{\n  \"mcpServers\": {\n  }\n}\n"),
			components.SelectField("policy", "On duplicate name", []string{"skip", "update", "rename"}, "skip"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			content, policy := sub.Values["content"], sub.Values["policy"]
			return func() tea.Msg {
				out, err := be.ImportMCPServers(content, policy)
				return components.OpDoneMsg{Tag: "mcp.import", Toast: fmt.Sprintf("Imported %s", plural(len(out), "server")), Err: err}
			}
		})
		return nil, true
	case "D":
		if srv := s.server(m, m.Selected()); srv != nil {
			id := srv.ID
			m.overlays.OpenConfirm(th, "Delete MCP server",
				fmt.Sprintf("Delete %q? Agents linked to it lose the server.", srv.Name),
				func() tea.Cmd {
					return components.Op("mcp.delete", "MCP server deleted", func() error { return be.DeleteMCPServer(id) })
				})
		}
		return nil, true
	case "enter":
		if srv := s.server(m, m.Selected()); srv != nil {
			m.detail = newMCPDetail(m.ctx, m.be, m.snap, srv.ID)
		}
		return nil, true
	}
	return nil, false
}

// updateMCPServer is the submit command of the edit form, shared with the detail.
func updateMCPServer(be backend.Backend, id string) components.SubmitFunc {
	return func(sub components.FormSubmitMsg) tea.Cmd {
		in := mcpInput(sub.Values)
		return components.Op("mcp.update", "MCP server updated", func() error {
			_, err := be.UpdateMCPServer(id, in)
			return err
		})
	}
}

// result turns a probe outcome into a toast and a refresh on the list.
func (mcpsSpec) result(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	pd, ok := msg.(probeDoneMsg)
	if !ok {
		return nil, false
	}
	if pd.err != nil {
		return components.ErrorToast("Probe failed: " + core.Message(pd.err)), true
	}
	text := fmt.Sprintf("Probe %s · %s · %s", pd.result.Status, plural(pd.result.ToolCount, "tool"), plural(pd.result.ResourceCount, "resource"))
	return tea.Batch(
		func() tea.Msg { return core.RefreshMsg{} },
		func() tea.Msg { return core.ToastMsg{Text: text, IsError: pd.result.Status != domain.MCPProbeStatusOK} },
	), true
}

func (mcpsSpec) conflict(*Model, components.OpDoneMsg) bool { return false }

// mcpForm builds the create/edit form; args are space-separated, env is one
// KEY=VALUE per line.
func mcpForm(th theme.Theme, title string, s *domain.MCPServer) components.Form {
	var name, desc, transport, command, args, url, env string
	transport = domain.MCPTransportStdio
	if s != nil {
		name, desc, transport, command, url = s.Name, s.Description, s.Transport, s.Command, s.URL
		args = strings.Join(s.Args, " ")
		env = formatEnv(s.Env)
	}
	return components.NewForm(th, title,
		components.TextField("name", "Name", name).Required(),
		components.TextField("description", "Description", desc),
		components.SelectField("transport", "Transport", []string{domain.MCPTransportStdio, domain.MCPTransportSSE, domain.MCPTransportStreamableHTTP}, transport),
		components.TextField("command", "Command (stdio)", command).Placeholder("npx"),
		components.TextField("args", "Arguments (space-separated)", args).Placeholder("-y @scope/server"),
		components.TextField("url", "URL (sse / streamable-http)", url).Placeholder("http://localhost:3000/mcp"),
		components.MultilineField("env", "Environment (KEY=VALUE per line)", env),
	)
}

func mcpInput(v map[string]string) domain.MCPServerInput {
	return domain.MCPServerInput{
		Name: v["name"], Description: v["description"], Transport: v["transport"],
		Command: v["command"], Args: strings.Fields(v["args"]), URL: v["url"], Env: parseEnv(v["env"]),
	}
}

func formatEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+env[k])
	}
	return strings.Join(lines, "\n")
}

func parseEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
