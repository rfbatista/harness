package catalog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

func snap() backend.Snapshot {
	f := backend.NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "coding_pool", RootDir: "/src/cp"}}
	f.Repositories = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "main-repo"}}
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer", Description: "reviews PRs", SkillIDs: []string{"k1"}, MCPServerIDs: []string{"m1", "m2"}}}
	f.Skills = []*domain.Skill{
		{ID: "k1", Name: "tdd-workflow", Files: []domain.SkillFile{{Path: "SKILL.md"}, {Path: "ref.md"}}},
		{ID: "k2", Name: "broken", Validation: []domain.ValidationIssue{{Message: "missing SKILL.md"}}},
	}
	f.MCPServers = []*domain.MCPServer{{ID: "m1", Name: "filesystem", Transport: domain.MCPTransportStdio, LastProbeStatus: domain.MCPProbeStatusOK, ToolCount: 7}}
	f.Settings[domain.SettingWorkspacesRoot] = "/wt"
	return backend.Load(f)
}

func ctx() core.Context { return core.Context{Theme: theme.Dark(), Width: 100, Height: 30} }

func view(k Kind) string {
	m := New(ctx(), k, backend.NewFake())
	m, _ = m.Update(core.SnapshotMsg{Snapshot: snap()})
	return m.View()
}

func TestAgentsListShowsNameAndCounts(t *testing.T) {
	v := view(Agents)
	for _, want := range []string{"reviewer", "reviews PRs", "1 skill", "2 MCP"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

func TestSkillsListShowsFileCountAndInvalidMarker(t *testing.T) {
	v := view(Skills)
	for _, want := range []string{"tdd-workflow", "2 files", "broken", "invalid"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

func TestMCPListShowsTransportAndProbe(t *testing.T) {
	v := view(MCPs)
	for _, want := range []string{"filesystem", "stdio", "ok", "7 tools"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

func TestProjectsListShowsRootAndRepoCountAndWorkspacesRoot(t *testing.T) {
	v := view(Projects)
	for _, want := range []string{"coding_pool", "/src/cp", "1 repo", "/wt"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
}

func TestEmptyStateIsExplicit(t *testing.T) {
	m := New(ctx(), Agents, backend.NewFake())
	m, _ = m.Update(core.SnapshotMsg{Snapshot: backend.Load(backend.NewFake())})
	if !strings.Contains(m.View(), "No agents yet") {
		t.Fatalf("empty state:\n%s", m.View())
	}
}

func TestSlashFiltersRows(t *testing.T) {
	m := New(ctx(), Skills, backend.NewFake())
	m, _ = m.Update(core.SnapshotMsg{Snapshot: snap()})
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "brok" {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v := m.View()
	if strings.Contains(v, "tdd-workflow") || !strings.Contains(v, "broken") {
		t.Fatalf("filter should keep only matching rows:\n%s", v)
	}
}
