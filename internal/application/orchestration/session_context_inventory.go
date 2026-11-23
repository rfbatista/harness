package orchestration

import (
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/builtinskills"
)

// delegateRefusedTools are the task server's tools a delegate of an architect
// is refused: they belong to the architect and the person (the tools answer
// ARCHITECT_ONLY and TASK_STATUS_OWNED_BY_ARCHITECT).
var delegateRefusedTools = []string{"update_task_status", "merge_session_branch", "list_status_checks"}

// contextToolsAndServers are the tools the session can use and the MCP servers
// it got, from the resolved configuration. Only names are read: an MCP
// server's environment, arguments and headers are never looked at (C2).
func contextToolsAndServers(in contextInput) ([]domain.ContextTool, []domain.ContextServer) {
	hidden := map[string][]string{} // server -> its hidden tools
	for _, ref := range in.Attached.Denied {
		if server, tool, ok := splitMCPTool(ref); ok {
			hidden[server] = append(hidden[server], tool)
		}
	}
	var tools []domain.ContextTool
	seen := map[string]bool{}
	add := func(name, state string) {
		if seen[name] {
			return
		}
		seen[name] = true
		t := domain.ContextTool{Name: name, Source: "claude_code", State: state}
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			t.Source = "mcp"
			t.Server, _, _ = strings.Cut(rest, "__")
		}
		tools = append(tools, t)
	}

	for _, name := range in.Cfg.AllowedTools {
		add(name, domain.ToolAllowed)
	}
	var servers []domain.ContextServer
	for _, spec := range in.Cfg.MCPServers {
		attached, reason := attributeServer(spec.Name, in)
		servers = append(servers, domain.ContextServer{
			Name: spec.Name, Attached: attached, AttachedReason: reason,
			Status: domain.MCPUnknown, StatusReason: unknownReason(in.Facts.Headless),
			HiddenTools: orEmptyStrings(hidden[spec.Name]),
		})
		for _, tool := range hidden[spec.Name] {
			add(llmkit.MCPToolRef(spec.Name, tool), domain.ToolHidden)
		}
		for _, tool := range in.KnownTools[spec.Name] {
			if !slices.Contains(hidden[spec.Name], tool) {
				add(llmkit.MCPToolRef(spec.Name, tool), domain.ToolOnDemand)
			}
		}
		if spec.Name == taskServerName && in.Role == domain.RoleDelegate {
			for _, tool := range delegateRefusedTools {
				ref := llmkit.MCPToolRef(spec.Name, tool)
				if i := slices.IndexFunc(tools, func(t domain.ContextTool) bool { return t.Name == ref }); i >= 0 {
					tools[i].State = domain.ToolRefused
					continue
				}
				add(ref, domain.ToolRefused)
			}
		}
	}
	return tools, servers
}

// splitMCPTool splits mcp__<server>__<tool>.
func splitMCPTool(ref string) (server, tool string, ok bool) {
	rest, ok := strings.CutPrefix(ref, "mcp__")
	if !ok {
		return "", "", false
	}
	server, tool, ok = strings.Cut(rest, "__")
	return server, tool, ok
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// attributeServer says how the session came to have an MCP server, and why.
func attributeServer(name string, in contextInput) (attached, reason string) {
	switch name {
	case taskServerName:
		return domain.AttachedBuiltIn, "Every session: the task channel"
	case servicesServerName:
		return domain.AttachedBuiltIn, "Every session: the project's services (ADR 0075)"
	case browserServerName:
		return domain.AttachedBuiltIn, "Every sandboxed session (ADR 0025)"
	case codegraphServerName:
		return domain.AttachedTokenSaver, "The codegraph token saver is on for this session"
	}
	for _, m := range in.Attached.Servers {
		if domain.Slug(m.Name) != name {
			continue
		}
		if in.Agent != nil && (slices.Contains(in.Agent.MCPServerIDs, m.ID) || slices.Contains(in.Agent.MCPServerIDs, m.Name)) {
			return domain.AttachedAgent, "Listed by the agent " + in.Agent.Name
		}
		return domain.AttachedProject, "Added in the project's MCP servers (ADR 0067)"
	}
	return domain.AttachedMode, "Brought by the session's mode"
}

// contextSkills are the skills the session has: the mode's and the agent's
// (worked out where the agent is resolved), and the repository's, which are
// the skill files among its delivered instruction files.
func contextSkills(in contextInput) []domain.ContextSkill {
	out := append([]domain.ContextSkill(nil), in.Skills...)
	for _, f := range in.Cfg.InstructionFiles {
		if f.Kind != llmkit.InstructionSkill || !strings.HasSuffix(f.Path, "/SKILL.md") {
			continue
		}
		parts := strings.Split(f.Path, "/")
		out = append(out, domain.ContextSkill{
			Name: parts[len(parts)-2], Source: "repository", Description: skillDescription(string(f.Content)),
		})
	}
	return out
}

// skillDescription is the description line of a SKILL.md's frontmatter.
func skillDescription(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if v, ok := strings.CutPrefix(l, "description:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// contextHooks are the hook rules of the session's claude settings: the
// event, the matcher and what the rule does, never its command.
func contextHooks(cfg llmkit.SessionConfig) []domain.ContextHook {
	dc, ok := cfg.Driver.(claude.Config)
	if !ok || dc.Settings == nil {
		return nil
	}
	events := make([]string, 0, len(dc.Settings.Hooks))
	for event := range dc.Settings.Hooks {
		events = append(events, string(event))
	}
	slices.Sort(events)
	var out []domain.ContextHook
	for _, event := range events {
		for _, m := range dc.Settings.Hooks[claude.HookEvent(event)] {
			rule := hookRule(event, m)
			out = append(out, domain.ContextHook{Event: event, Matcher: m.Matcher, Rule: rule})
		}
	}
	return out
}

// hookRule says in a line what a hook matcher does, from the endpoint its
// command calls. A command it does not know is a command hook, unnamed.
func hookRule(event string, m claude.HookMatcher) string {
	var cmds []string
	for _, h := range m.Hooks {
		cmds = append(cmds, h.Command)
	}
	all := strings.Join(cmds, "\n")
	switch {
	case strings.Contains(all, "/branch_gate"):
		return "Asks you before a branch action or a push, and holds the command until you answer"
	case strings.Contains(all, "/architecture/check"):
		if event == "PostToolUse" {
			return "Checks each edit against the repository architecture's rules"
		}
		return "Checks the tool call against the repository architecture's layers and rules"
	case strings.Contains(all, "/commit?hook="):
		return "Stores a file written in the virtual shell"
	case strings.Contains(all, "/check?hook="):
		return "Checks a write against the virtual shell and refuses one it cannot store"
	case strings.Contains(all, "worktree-read"), strings.Contains(all, "WorktreeRead"):
		return "Narrows the architect's read access to its task's worktrees"
	}
	return "Runs a command hook"
}

// configResolution is what sessionConfig decided besides the configuration:
// the version of each delivered instruction file, the MCP servers the catalog
// and the project gave the session, and the tools the catalog knows.
type configResolution struct {
	versions map[string]string
	attached mcpAttachment
	known    map[string][]string
}

// classifySkills says where each of a session's skills comes from: the mode,
// the server's built-in set, or the agent (ADR 0088).
func classifySkills(skills, mode []domain.Skill) []domain.ContextSkill {
	inMode := map[string]bool{}
	for _, s := range mode {
		inMode[domain.Slug(s.Name)] = true
	}
	out := make([]domain.ContextSkill, 0, len(skills))
	for _, s := range skills {
		source := "agent"
		slug := domain.Slug(s.Name)
		builtin, isBuiltin := builtinskills.Get(slug)
		if inMode[slug] {
			source = "mode"
		} else if isBuiltin {
			source = "built_in"
		}
		description := s.Description
		if description == "" && isBuiltin {
			description = builtin.Description
		}
		out = append(out, domain.ContextSkill{Name: s.Name, Source: source, Description: description})
	}
	return out
}

// unknownReason says why a server has no status: a terminal session's claude
// reports none (the contract's words); a headless session's claude reports it
// at start, and until it has the status is just not reported.
func unknownReason(headless bool) string {
	if headless {
		return "claude did not report it"
	}
	return domain.MCPStatusUnknownReason
}
