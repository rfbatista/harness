package orchestration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
)

func serverByName(e domain.ContextEntry, name string) (domain.ContextServer, bool) {
	for _, s := range e.MCPServers {
		if s.Name == name {
			return s, true
		}
	}
	return domain.ContextServer{}, false
}

func toolByName(e domain.ContextEntry, name string) (domain.ContextTool, bool) {
	for _, t := range e.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return domain.ContextTool{}, false
}

func inventoryInput() contextInput {
	project := &domain.Project{
		MCPServers:       []string{"postgres"},
		MCPToolOverrides: map[string]map[string]string{"postgres": {"drop_table": domain.ToolDisabled}},
	}
	agent := &domain.Agent{Name: "go-developer", MCPServerIDs: []string{"github"}}
	catalog := []*domain.MCPServer{{ID: "github", Name: "github"}, {ID: "postgres", Name: "postgres"}}
	attached := resolveMCP(agent, project, catalog, nil, nil)
	cfg := llmkit.SessionConfig{
		AllowedTools: []string{"Bash(git status)", "mcp__task", "mcp__github__get_issue"},
		MCPServers: []llmkit.MCPServerSpec{
			{Name: "task", Transport: "http", URL: "http://x/mcp/task/s1", Headers: map[string]string{"Authorization": "Bearer t"}},
			{Name: "github", Transport: "http", URL: "https://api/mcp", Headers: map[string]string{"Authorization": "Bearer gh"}},
			{Name: "postgres", Transport: "stdio", Command: "pg", Env: map[string]string{"PGPASSWORD": "hunter2"}},
			{Name: "browser", Transport: "stdio", Command: "npx"},
			{Name: "codegraph", Transport: "stdio", Command: "cg"},
			{Name: "services", Transport: "http", URL: "http://x/mcp/services/s1"},
		},
	}
	return contextInput{
		Kind: domain.ContextEntryStart, ID: "e1", Cfg: cfg, Role: domain.RoleDelegate,
		Agent: agent, Project: project, Attached: attached,
		KnownTools: map[string][]string{"github": {"get_issue", "create_pull_request", "push_files"}, "postgres": {"query", "drop_table"}},
	}
}

func TestBuildContextEntry_EachServerIsAttributedToWhatBroughtIt(t *testing.T) {
	e := buildContextEntry(inventoryInput())
	want := map[string]string{
		"task": domain.AttachedBuiltIn, "browser": domain.AttachedBuiltIn, "services": domain.AttachedBuiltIn,
		"codegraph": domain.AttachedTokenSaver, "github": domain.AttachedAgent, "postgres": domain.AttachedProject,
	}
	if len(e.MCPServers) != len(want) {
		t.Fatalf("servers = %+v", e.MCPServers)
	}
	for name, attached := range want {
		s, ok := serverByName(e, name)
		if !ok || s.Attached != attached || s.AttachedReason == "" {
			t.Errorf("%s = %+v (found %v), want attached %s with a reason", name, s, ok, attached)
		}
	}
}

func TestBuildContextEntry_ATerminalSessionsServersHaveNoStatus(t *testing.T) {
	e := buildContextEntry(inventoryInput())
	for _, s := range e.MCPServers {
		if s.Status != domain.MCPUnknown || s.StatusReason != domain.MCPStatusUnknownReason {
			t.Errorf("%s = status %q reason %q", s.Name, s.Status, s.StatusReason)
		}
	}
}

func TestBuildContextEntry_ToolsCarryTheirState(t *testing.T) {
	e := buildContextEntry(inventoryInput())
	cases := map[string]string{
		"Bash(git status)":                 domain.ToolAllowed,
		"mcp__github__get_issue":           domain.ToolAllowed,
		"mcp__github__create_pull_request": domain.ToolOnDemand,
		"mcp__github__push_files":          domain.ToolOnDemand,
		"mcp__postgres__query":             domain.ToolOnDemand,
		"mcp__postgres__drop_table":        domain.ToolHidden,
		"mcp__task__update_task_status":    domain.ToolRefused,
		"mcp__task__merge_session_branch":  domain.ToolRefused,
	}
	for name, state := range cases {
		if tool, ok := toolByName(e, name); !ok || tool.State != state {
			t.Errorf("%s = %+v (found %v), want %s", name, tool, ok, state)
		}
	}
	if tool, _ := toolByName(e, "mcp__github__push_files"); tool.Source != "mcp" || tool.Server != "github" {
		t.Errorf("source/server = %+v", tool)
	}
	if tool, _ := toolByName(e, "Bash(git status)"); tool.Source != "claude_code" || tool.Server != "" {
		t.Errorf("claude code tool = %+v", tool)
	}
	// The whole-server allowance stays what it is: a bare server name.
	if tool, ok := toolByName(e, "mcp__task"); !ok || tool.State != domain.ToolAllowed || tool.Server != "task" {
		t.Errorf("mcp__task = %+v (found %v)", tool, ok)
	}
	gh, _ := serverByName(e, "postgres")
	if len(gh.HiddenTools) != 1 || gh.HiddenTools[0] != "drop_table" {
		t.Errorf("hidden = %v", gh.HiddenTools)
	}
}

func TestBuildContextEntry_AnArchitectIsRefusedNothingOfTheTaskServer(t *testing.T) {
	in := inventoryInput()
	in.Role = domain.RoleArchitect
	e := buildContextEntry(in)
	if tool, ok := toolByName(e, "mcp__task__update_task_status"); ok {
		t.Errorf("an architect is refused %+v", tool)
	}
}

func TestBuildContextEntry_SkillsCarryTheirSource(t *testing.T) {
	in := inventoryInput()
	in.Skills = []domain.ContextSkill{{Name: "review", Source: "mode", Description: "Review a branch"}}
	in.Cfg.InstructionFiles = []llmkit.InstructionFile{
		{Kind: llmkit.InstructionSkill, Path: ".claude/skills/release/SKILL.md", Content: []byte("---\nname: release\ndescription: Cut a release\n---\nbody")},
		{Kind: llmkit.InstructionSkill, Path: ".claude/skills/release/notes.md", Content: []byte("notes")},
	}
	in.Repository = "harness"
	e := buildContextEntry(in)
	by := map[string]domain.ContextSkill{}
	for _, s := range e.Skills {
		by[s.Name] = s
	}
	if len(e.Skills) != 2 || by["review"].Source != "mode" || by["review"].Description != "Review a branch" {
		t.Errorf("skills = %+v", e.Skills)
	}
	if r := by["release"]; r.Source != "repository" || r.Description != "Cut a release" {
		t.Errorf("repository skill = %+v", r)
	}
}

func TestBuildContextEntry_HooksAreRulesNeverCommands(t *testing.T) {
	in := inventoryInput()
	dc := claude.Config{Settings: &claude.Settings{Hooks: BranchGateHooks("http://127.0.0.1:8080/api/sessions/s1/branch_gate", 0)}}
	merged, _ := withHooks(dc, ArchitectureHooks("http://127.0.0.1:8080/api/sessions/s1/architecture"))
	in.Cfg.Driver = merged
	e := buildContextEntry(in)
	if len(e.Hooks) != 3 {
		t.Fatalf("hooks = %+v", e.Hooks)
	}
	for _, h := range e.Hooks {
		if h.Event == "" || h.Matcher == "" || h.Rule == "" {
			t.Errorf("hook = %+v", h)
		}
		if strings.Contains(h.Rule, "curl") || strings.Contains(h.Rule, "127.0.0.1") {
			t.Errorf("a hook rule shows its command: %+v", h)
		}
	}
	by := map[string]string{}
	for _, h := range e.Hooks {
		by[h.Event+" "+h.Matcher] = h.Rule
	}
	if !strings.Contains(by["PreToolUse "+BranchGateTools], "branch") {
		t.Errorf("branch gate rule = %q", by["PreToolUse "+BranchGateTools])
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "Bearer") || strings.Contains(string(b), "PGPASSWORD") {
		t.Errorf("entry leaks a secret: %s", b)
	}
}
