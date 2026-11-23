package orchestration

import (
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The tests here cover the start surfaces and rule corners the first mode
// agents tests leave out (ADR 0103).

var startModes = []domain.SessionMode{
	domain.SessionModeDefault, domain.SessionModeArchitect, domain.SessionModeImplement, domain.SessionModeDesign,
	domain.SessionModeProduct, domain.SessionModeReview, domain.SessionModeTest, domain.SessionModeQA, domain.SessionModeOperate,
}

// Each mode reads its own entry: closing one mode refuses there and only there.
func TestSessionStart_EachModeIsClosedByItsOwnEntryAndByNoOther(t *testing.T) {
	for _, closed := range startModes {
		svc, _, _, settings := modeAgentsWorld(t)
		settings.selectMode(closed, false)
		for _, mode := range startModes {
			req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", AgentID: "a1", Mode: string(mode)}
			if mode != domain.SessionModeProduct {
				req.TicketID = "tk1"
			}
			_, _, err := svc.StartInteractive(t.Context(), req)
			if mode == closed {
				if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
					t.Errorf("closed %q, start in %q: err = %v", closed, mode, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("closed %q, start in %q was refused: %v", closed, mode, err)
			}
		}
	}
}

// The message names the mode by its name and the agent by its name.
func TestSessionStart_TheRefusalNamesTheModeAndTheAgentAndWhoChangesIt(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeQA, true, "someone-else")
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "qa"})
	if err == nil {
		t.Fatal("not refused")
	}
	for _, want := range []string{"AGENT_NOT_AVAILABLE_IN_MODE", "backend", "QA", "server settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
}

// The design-system surface: refused and allowed.
func TestDesignSystemStart_IsCheckedAgainstTheDesignSystemModesAgents(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	svc.catalog.DesignSystems = brandOne{}
	svc.Terminals = termhost.New(shell.Direct{}, ptyunix.New(), scriptAgent{script: `cat`})

	settings.selectMode(domain.SessionModeDesignSystem, true, "someone-else")
	_, err := svc.StartDesignSession(t.Context(), "ds1", "a1", "tokens")
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.Contains(err.Error(), "Design system") {
		t.Fatalf("agent outside the list: err = %v", err)
	}
	// No agent is refused when the flag is off, allowed when on.
	settings.selectMode(domain.SessionModeDesignSystem, false, "backend")
	_, err = svc.StartDesignSession(t.Context(), "ds1", "", "tokens")
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.Contains(err.Error(), "a session with no agent") {
		t.Fatalf("no agent: err = %v", err)
	}
	// A listed agent starts.
	if _, err := svc.StartDesignSession(t.Context(), "ds1", "a1", "tokens"); err != nil {
		t.Fatalf("listed agent: %v", err)
	}
	// Closing another mode leaves the design-system start alone.
	settings.selectMode(domain.SessionModeDesignSystem, true, "backend")
	settings.selectMode(domain.SessionModeDefault, false)
	if _, err := svc.StartDesignSession(t.Context(), "ds1", "a1", "tokens"); err != nil {
		t.Fatalf("design system with default closed: %v", err)
	}
}

// A brainstorm (product mode, no task) is allowed when its mode lists the agent
// and refused for no agent when the flag is off.
func TestSessionStart_ABrainstormIsAllowedForAListedAgentAndRefusedForNoAgentWhenOff(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeProduct, false, "backend")
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", AgentID: "a1", Mode: "product"}); err != nil {
		t.Fatalf("listed agent: %v", err)
	}
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "product"})
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" || !strings.Contains(err.Error(), "a session with no agent") {
		t.Fatalf("no agent: err = %v", err)
	}
}

// A refused start leaves no session behind.
func TestSessionStart_ARefusedStartRecordsNoSession(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeReview, false)
	before := len(svc.sessions.List(t.Context(), ports.SessionFilter{}))
	if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "review"}); err == nil {
		t.Fatal("not refused")
	}
	if after := len(svc.sessions.List(t.Context(), ports.SessionFilter{})); after != before {
		t.Fatalf("sessions %d -> %d", before, after)
	}
}

// The check order holds on the headless start too.
func TestSessionStart_HeadlessChecksTheModeBeforeEnabledAndAssigned(t *testing.T) {
	svc, res, as, settings := modeAgentsWorld(t)
	res.proj.EnabledAgentsMode = domain.AgentsSelected
	res.proj.EnabledAgents = []string{"someone-else"}
	as.unassign("r1", "backend")
	settings.selectMode(domain.SessionModeDefault, false)
	req := StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "x", AgentID: "a1"}

	if _, err := svc.Start(t.Context(), req); codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("err = %v", err)
	}
	settings.selectMode(domain.SessionModeDefault, false, "backend")
	if _, err := svc.Start(t.Context(), req); codeOf(err) != "AGENT_NOT_ENABLED" {
		t.Fatalf("err = %v", err)
	}
}

// No agent is not subject to enabled or assigned agents, only to its mode.
func TestSessionStart_NoAgentIsRefusedByTheModeEvenWithOpenProjectAndRepository(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings.selectMode(domain.SessionModeTest, false, "backend")
	_, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "test"})
	if codeOf(err) != "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("err = %v", err)
	}
}

// A stored value that is not the mode agents JSON never closes a mode.
func TestSessionStart_AGarbledStoredValueIsAllAgents(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	settings[domain.ModeAgentsKey(domain.SessionModeReview)] = "{not json"
	for _, agent := range []string{"a1", ""} {
		if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: agent, Mode: "review"}); err != nil {
			t.Fatalf("agent %q: %v", agent, err)
		}
	}
}

// A server that never wired mode agents refuses nothing.
func TestSessionStart_WithoutModeAgentsWiredNothingIsRefused(t *testing.T) {
	svc, _, _, _ := assignedWorld(t)
	for _, agent := range []string{"a1", ""} {
		if _, _, err := svc.StartInteractive(t.Context(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: agent, Mode: "review"}); err != nil {
			t.Fatalf("agent %q: %v", agent, err)
		}
	}
}

// ResumeSession, the person's click, is not refused either, and an agent
// that is no longer listed or a closed mode do not stop the resume.
func TestSessionResume_TheClickResumeIsNotRefusedByAClosedMode(t *testing.T) {
	svc, _, _, settings := modeAgentsWorld(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1", Mode: "review"})
	if _, err := svc.EndInteractive(t.Context(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	settings.selectMode(domain.SessionModeReview, false)
	if err := svc.ResumeSession(t.Context(), sess.ID); codeOf(err) == "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("resume refused by the mode: %v", err)
	}
	// A resume that names a mode is not checked either.
	if _, _, err := svc.ResumeInteractive(t.Context(), ports.ResumeRequest{SessionID: sess.ID}); codeOf(err) == "AGENT_NOT_AVAILABLE_IN_MODE" {
		t.Fatalf("resume refused by the mode: %v", err)
	}
}
