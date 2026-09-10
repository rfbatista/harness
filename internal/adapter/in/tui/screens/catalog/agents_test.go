package catalog

import (
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

func agentsFixture() (*backend.Fake, Model) {
	f := backend.NewFake()
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer", Description: "reviews", SkillIDs: []string{"k1"}}}
	f.Skills = []*domain.Skill{{ID: "k1", Name: "tdd"}, {ID: "k2", Name: "docs"}}
	f.MCPServers = []*domain.MCPServer{{ID: "m1", Name: "filesystem"}}
	m := withSnap(New(ctx(), Agents, f), f)
	return f, m
}

func TestAgentsNewOpensFormAndCreates(t *testing.T) {
	f, m := agentsFixture()
	m, _ = press(m, ch('n'))
	if !m.Capturing() || !strings.Contains(m.View(), "New agent") {
		t.Fatalf("form should be open and capturing:\n%s", m.View())
	}
	m = typeKeys(m, "planner")
	m, root := press(m, enter())
	if len(f.Agents) != 2 || f.Agents[1].Name != "planner" {
		t.Fatalf("agent not created: %+v", f.Agents)
	}
	if m.Capturing() || !hasRefresh(root) {
		t.Fatalf("form should close and ask for a refresh; capturing=%v root=%v", m.Capturing(), root)
	}
}

func TestAgentsEditPrefillsAndUpdates(t *testing.T) {
	f, m := agentsFixture()
	m, _ = press(m, ch('e'))
	if !strings.Contains(m.View(), "reviewer") {
		t.Fatalf("edit form should be prefilled:\n%s", m.View())
	}
	m = typeKeys(m, "-2")
	_, _ = press(m, enter())
	if f.Agents[0].Name != "reviewer-2" || f.Agents[0].SkillIDs[0] != "k1" {
		t.Fatalf("update should keep capabilities: %+v", f.Agents[0])
	}
}

func TestAgentsCapabilitiesToggleSkillsAndMCP(t *testing.T) {
	f, m := agentsFixture()
	m, _ = press(m, ch('c'))
	v := m.View()
	if !strings.Contains(v, "[x] tdd") || !strings.Contains(v, "[ ] docs") || !strings.Contains(v, "[ ] filesystem") {
		t.Fatalf("capabilities checklist:\n%s", v)
	}
	m, _ = press(m, ch(' '))    // untoggle tdd
	m, _ = press(m, tea_down()) // docs
	m, _ = press(m, ch(' '))    // toggle docs
	m, _ = press(m, tab())      // mcp list
	m, _ = press(m, ch(' '))    // toggle filesystem
	_, _ = press(m, enter())
	a := f.Agents[0]
	if len(a.SkillIDs) != 1 || a.SkillIDs[0] != "k2" || len(a.MCPServerIDs) != 1 || a.MCPServerIDs[0] != "m1" {
		t.Fatalf("capabilities not saved: %+v", a)
	}
}

func TestAgentsDeleteAsksThenDeletes(t *testing.T) {
	f, m := agentsFixture()
	m, _ = press(m, ch('D'))
	if !strings.Contains(m.View(), "reviewer") || !m.Capturing() {
		t.Fatalf("confirm should name the agent:\n%s", m.View())
	}
	m, root := press(m, ch('y'))
	if len(f.Agents) != 0 || !hasRefresh(root) {
		t.Fatalf("agent should be deleted: %+v", f.Agents)
	}
	if m.Capturing() {
		t.Fatal("confirm should close")
	}
}

func TestAgentsDeleteCancelKeeps(t *testing.T) {
	f, m := agentsFixture()
	m, _ = press(m, ch('D'))
	m, _ = press(m, ch('n'))
	if len(f.Agents) != 1 || m.Capturing() {
		t.Fatal("cancel should keep the agent and close the prompt")
	}
}

func TestAgentsInvalidInputStaysOnFormWithMessage(t *testing.T) {
	f, m := agentsFixture()
	f.Fail = &domain.StructuredError{Code: "INVALID_INPUT", Message: "name must be unique"}
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "reviewer")
	m, root := press(m, enter())
	if !m.Capturing() || !strings.Contains(m.View(), "name must be unique") {
		t.Fatalf("form should stay open with the error:\n%s", m.View())
	}
	if hasRefresh(root) {
		t.Fatal("no refresh on failure")
	}
}

func TestAgentsUnexpectedErrorClosesFormWithToast(t *testing.T) {
	f, m := agentsFixture()
	f.Fail = &domain.StructuredError{Code: "INTERNAL", Message: "db locked"}
	m, _ = press(m, ch('n'))
	m = typeKeys(m, "x")
	m, root := press(m, enter())
	if m.Capturing() {
		t.Fatal("form should close on an unexpected error")
	}
	found := false
	for _, r := range root {
		if tm, ok := r.(core.ToastMsg); ok && strings.Contains(tm.Text, "db locked") && tm.IsError {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an error toast, got %v", root)
	}
}

func TestAgentsEscClosesForm(t *testing.T) {
	_, m := agentsFixture()
	m, _ = press(m, ch('n'))
	m, _ = press(m, esc())
	if m.Capturing() {
		t.Fatal("esc should close the form")
	}
}
