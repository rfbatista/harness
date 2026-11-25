package sessions

import (
	"encoding/json"
	"reflect"
	"testing"

	"operators-mcp/internal/domain"
)

func TestNewSessionForm_ReceivesEveryModesAgentsBesideTheModeModels(t *testing.T) {
	form := NewSessionFormOf(nil, nil)
	if len(form.ModeAgents) != 10 {
		t.Fatalf("modes = %v", form.ModeAgents)
	}
	for mode, got := range form.ModeAgents {
		if got.State != "all" || len(got.Agents) != 0 || !got.NoAgent {
			t.Errorf("mode %q = %+v, want All agents", mode, got)
		}
	}
	if _, ok := form.ModeAgents[""]; !ok {
		t.Error("the default mode is the empty mode value")
	}
}

func TestNewSessionForm_ReadsTheStoredModeAgentsAndSeedsThemAsJSON(t *testing.T) {
	settings := map[string]string{
		domain.ModeAgentsKey(domain.SessionModeReview):  domain.ModeAgentsValue("selected", []string{"code-reviewer"}, false),
		domain.ModeAgentsKey(domain.SessionModeDefault): domain.ModeAgentsValue("selected", nil, true),
	}
	form := NewSessionFormOf(nil, nil).WithModeAgents(func(k string) string { return settings[k] })

	var seed map[string]struct {
		State   string   `json:"state"`
		Agents  []string `json:"agents"`
		NoAgent bool     `json:"no_agent"`
	}
	if err := json.Unmarshal([]byte(form.ModeAgentsSeed()), &seed); err != nil {
		t.Fatal(err)
	}
	if r := seed["review"]; r.State != "selected" || !reflect.DeepEqual(r.Agents, []string{"code-reviewer"}) || r.NoAgent {
		t.Errorf("review = %+v", r)
	}
	if d := seed[""]; d.State != "selected" || len(d.Agents) != 0 || !d.NoAgent {
		t.Errorf("default = %+v", d)
	}
	if q := seed["qa"]; q.State != "all" || !q.NoAgent {
		t.Errorf("qa = %+v", q)
	}
}
