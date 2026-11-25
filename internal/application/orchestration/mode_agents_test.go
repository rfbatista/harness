package orchestration

import (
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeModeSettings holds the settings the mode agents live in.
type fakeModeSettings map[string]string

func (f fakeModeSettings) Setting(key string) string { return f[key] }

// selectMode makes the mode Selected agents with a list and the No agent flag.
func (f fakeModeSettings) selectMode(mode domain.SessionMode, noAgent bool, agents ...string) {
	f[domain.ModeAgentsKey(mode)] = domain.ModeAgentsValue(domain.AgentsSelected, agents, noAgent)
}

func modeAgentsWorld(t *testing.T) (*Service, *fakeResolver, *fakeAssignments, fakeModeSettings) {
	t.Helper()
	svc, res, as, _ := assignedWorld(t)
	settings := fakeModeSettings{}
	svc.UseModeAgents(settings)
	return svc, res, as, settings
}

func TestSessionStart_EveryModeAcceptsEveryAgentWhileNothingIsSet(t *testing.T) {
	svc, _, _, _ := modeAgentsWorld(t)
	for _, mode := range []string{"", "architect", "implement", "review"} {
		for _, agent := range []string{"a1", ""} {
			if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: agent, Mode: mode}); err != nil {
				t.Fatalf("mode %q agent %q: %v", mode, agent, err)
			}
		}
	}
}

func TestSessionStart_RefusesAnAgentOutsideTheModesSelectedAgentsBeforeProvisioning(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	prov := svc.workspaces.(*fakeProvisioner)
	settings.selectMode(domain.SessionModeReview, true, "someone-else")

	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "review"})
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.HasPrefix(err.Error(), "AGENT_NOT_AVAILABLE_IN_MODE:") {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"backend", "Reviewer"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q does not name %q", err.Error(), want)
		}
	}
	if len(prov.created) != 0 {
		t.Fatalf("a refused start provisioned a worktree: %v", prov.created)
	}

	// The same agent starts in a mode that is still All agents.
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "test"}); err != nil {
		t.Fatalf("test mode: %v", err)
	}
	// And a listed agent starts in the selected one.
	settings.selectMode(domain.SessionModeReview, true, "backend")
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "review"}); err != nil {
		t.Fatalf("listed agent: %v", err)
	}
}

func TestSessionStart_NoAgentStartsOnlyWhereTheSelectedListHoldsIt(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "review"}

	settings.selectMode(domain.SessionModeReview, false, "backend")
	_, _, err := svc.StartInteractive(t.Context(), req)
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.Contains(err.Error(), "a session with no agent") {
		t.Fatalf("err = %v", err)
	}

	settings.selectMode(domain.SessionModeReview, true, "backend")
	if _, _, err := svc.StartInteractive(t.Context(), req); err != nil {
		t.Fatalf("no agent allowed: %v", err)
	}
}

func TestSessionStart_AnEmptySelectedListClosesTheMode(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeDefault, false)
	for _, agent := range []string{"a1", ""} {
		_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: agent})
		if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
			t.Fatalf("agent %q: err = %v", agent, err)
		}
	}
	// The headless start runs in the default mode.
	_, err := svc.Start(t.Context(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "x", AgentID: "a1"})
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("headless err = %v", err)
	}
}

func TestSessionStart_TheModeIsCheckedBeforeEnabledAndAssigned(t *testing.T) {
	svc, res, as, settings := modeAgentsWorld(t)
	res.proj.EnabledAgentsMode = domain.AgentsSelected
	res.proj.EnabledAgents = []string{"someone-else"}
	as.unassign("r1", "backend")
	settings.selectMode(domain.SessionModeReview, true)
	req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "review"}

	if _, _, err := svc.StartInteractive(t.Context(), req); codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("err = %v", err)
	}
	// Open the mode: the project's list is the next rule to speak.
	settings.selectMode(domain.SessionModeReview, true, "backend")
	if _, _, err := svc.StartInteractive(t.Context(), req); codeOf(err) != "AGENT_NOT_ENABLED" {
		t.Fatalf("err = %v", err)
	}
	res.proj.EnabledAgentsMode = domain.AgentsAll
	if _, _, err := svc.StartInteractive(t.Context(), req); codeOf(err) != "AGENT_NOT_ASSIGNED" {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionStart_AnUnknownAgentIsRefusedBeforeTheMode(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeDefault, false)
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "ghost"})
	if err == nil || codeOf(err) == "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionResume_IsNotRefusedWhenTheModeNoLongerAcceptsTheAgent(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if _, err := svc.EndInteractive(t.Context(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}

	settings.selectMode(domain.SessionModeDefault, false)
	if _, _, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

func TestSessionStart_ABrainstormInProductModeIsCheckedAgainstTheProductModesAgents(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeProduct, false, "someone-else")
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", AgentID: "a1", Mode: "product"})
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.Contains(err.Error(), "Product") {
		t.Fatalf("err = %v", err)
	}
}
