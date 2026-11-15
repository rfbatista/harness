package orchestration

import (
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/builtinskills"
)

func agentSkill(name, body string) domain.Skill {
	return domain.Skill{Name: name, Files: []domain.SkillFile{{Path: domain.SkillFileName, Content: domain.SkillMarkdownFor(name, "agent copy", body)}}}
}

// A plain session whose agent names a built-in skill gets it, with its
// reference files, from the embedded set.
func TestStartInteractive_AnAgentNamingABuiltInSkillGetsItWithoutAMode(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {ID: "a1", SkillIDs: []string{"task-run-app"}}}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Prompt: "run it"})

	want, _ := builtinskills.Get("task-run-app")
	if got := pluginSkill(t, launch, "task-run-app"); !strings.Contains(got, "name: task-run-app") {
		t.Errorf("task-run-app SKILL.md = %q", got)
	}
	for _, f := range want.Files {
		if f.Path != domain.SkillFileName {
			pluginFile(t, launch, "task-run-app", f.Path)
		}
	}
}

// A skill the mode and the agent both carry reaches the session once, with
// the mode's content.
func TestStartInteractive_ASkillOfTheModeAndTheAgentIsDeliveredOnce(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {
		ID: "a1", SkillIDs: []string{"task-run-app"}, Skills: []domain.Skill{agentSkill("task-run-app", "AGENT BODY")},
	}}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "operate"})

	got := pluginSkill(t, launch, "task-run-app")
	if !strings.Contains(got, "# Run the task's application") || strings.Contains(got, "AGENT BODY") {
		t.Errorf("task-run-app is not the mode's copy:\n%s", got)
	}
	pluginSkill(t, launch, "task-operate")
}

// The agent's own skills keep working beside a built-in one.
func TestStartInteractive_AnAgentGetsItsCatalogSkillAndABuiltInOne(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {
		ID: "a1", SkillIDs: []string{"review", "task-run-app"}, Skills: []domain.Skill{agentSkill("review", "REVIEW BODY")},
	}}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})

	if !strings.Contains(pluginSkill(t, launch, "review"), "REVIEW BODY") {
		t.Error("the catalog skill is lost")
	}
	pluginSkill(t, launch, "task-run-app")
}

// A catalog skill that carries a built-in name never replaces the built-in one.
func TestStartInteractive_ACatalogSkillShadowingABuiltInNeverWins(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {
		ID: "a1", SkillIDs: []string{"task-tdd"}, Skills: []domain.Skill{agentSkill("task-tdd", "SHADOW BODY")},
	}}
	_, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})

	if got := pluginSkill(t, launch, "task-tdd"); strings.Contains(got, "SHADOW BODY") {
		t.Errorf("the shadow won:\n%s", got)
	}
}
