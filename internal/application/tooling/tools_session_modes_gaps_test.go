package tooling

import (
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Corners of list_agents and start_task_session the first mode agents tests
// leave out (ADR 0103).

var allModeValues = []string{"", "architect", "implement", "design", "product", "designsystem", "review", "test", "qa", "operate"}

// A Selected list that is empty closes the mode to every agent and to no agent.
func TestListAgents_AnEmptySelectedListMakesEveryAgentAndNoAgentUnavailableThere(t *testing.T) {
	m := listAgentsWithModes(t, fakeModeSettings{
		domain.ModeAgentsKey(domain.SessionModeQA): domain.ModeAgentsValue("selected", nil, false),
	})
	for _, a := range m["agents"].([]any) {
		if got := modeValuesOf(a.(map[string]any)["unavailable_in_modes"]); !reflect.DeepEqual(got, []string{"qa"}) {
			t.Errorf("%v unavailable_in_modes = %v", a.(map[string]any)["name"], got)
		}
	}
	if got := modeValuesOf(m["no_agent_unavailable_in_modes"]); !reflect.DeepEqual(got, []string{"qa"}) {
		t.Errorf("no_agent_unavailable_in_modes = %v", got)
	}
}

// Every mode closed: the answer lists all ten values in the contract's order.
func TestListAgents_EveryModeClosedListsTheTenValuesInOrder(t *testing.T) {
	s := fakeModeSettings{}
	for _, mode := range allModeValues {
		s[domain.ModeAgentsKey(domain.SessionMode(mode))] = domain.ModeAgentsValue("selected", nil, false)
	}
	m := listAgentsWithModes(t, s)
	if got := modeValuesOf(m["no_agent_unavailable_in_modes"]); !reflect.DeepEqual(got, allModeValues) {
		t.Errorf("no_agent_unavailable_in_modes = %v", got)
	}
	for _, a := range m["agents"].([]any) {
		if got := modeValuesOf(a.(map[string]any)["unavailable_in_modes"]); !reflect.DeepEqual(got, allModeValues) {
			t.Errorf("unavailable_in_modes = %v", got)
		}
	}
}

// With No agent on and the agent listed the mode is open to both.
func TestListAgents_AModeThatListsTheAgentAndNoAgentIsUnavailableToNeither(t *testing.T) {
	m := listAgentsWithModes(t, fakeModeSettings{
		domain.ModeAgentsKey(domain.SessionModeTest): domain.ModeAgentsValue("selected", []string{"reviewer", "designer"}, true),
	})
	for _, a := range m["agents"].([]any) {
		if got := modeValuesOf(a.(map[string]any)["unavailable_in_modes"]); len(got) != 0 {
			t.Errorf("unavailable_in_modes = %v", got)
		}
	}
	if got := modeValuesOf(m["no_agent_unavailable_in_modes"]); len(got) != 0 {
		t.Errorf("no_agent_unavailable_in_modes = %v", got)
	}
}

// Fields are present, as empty lists and not as null, when nothing is set.
func TestListAgents_NothingSetTheFieldsAreEmptyListsNotNull(t *testing.T) {
	m := listAgentsWithModes(t, fakeModeSettings{})
	if v, ok := m["no_agent_unavailable_in_modes"]; !ok || v == nil {
		t.Errorf("no_agent_unavailable_in_modes = %v (present %v)", v, ok)
	}
	for _, a := range m["agents"].([]any) {
		if v, ok := a.(map[string]any)["unavailable_in_modes"]; !ok || v == nil {
			t.Errorf("unavailable_in_modes = %v (present %v)", v, ok)
		}
	}
}

// A refusal for a session with no agent also reads on its own over MCP.
func TestStartTaskSession_ANoAgentRefusalSaysWhatWasRefusedAndWhoChangesIt(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	starter.err = &domain.StructuredError{Code: "AGENT_NOT_AVAILABLE_IN_MODE", Message: domain.ModeAgentNotAvailableMessage("Tester", "")}
	_, err := start(map[string]any{"prompt": "x", "mode": "test"})
	if err == nil {
		t.Fatal("the refusal was swallowed")
	}
	for _, want := range []string{"AGENT_NOT_AVAILABLE_IN_MODE", "a session with no agent", "Tester", "Only the person", "server settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
}

// The mode the architect asks for reaches the start, so the check has it.
func TestStartTaskSession_TheRequestedModeAndAgentReachTheStart(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": "x", "agent": "reviewer", "mode": "qa"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0]; got.Mode != "qa" || got.AgentID != "a-rev" {
		t.Fatalf("request = %+v", got)
	}
}
