//go:build !windows

package app

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// designSessionServer is the server as it runs, with a project, its
// repository, an agent and a design system.
type designSessionServer struct {
	*shellE2E
	agent string
	ds    string
}

func newDesignSessionServer(t *testing.T) *designSessionServer {
	t.Helper()
	e := newShellE2E(t)
	// The fake agent stays up, so a session counts as running until it ends.
	if err := os.WriteFile(e.cfg.ClaudeBin, []byte("#!/bin/sh\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Nested sandboxes do not start in every environment the suite runs in.
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		t.Fatal(err)
	}
	ag, err := e.cat.Agents.CreateAgent(e.ctx, "designer", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := e.cat.DesignSystems.CreateDesignSystem(e.ctx, e.project, "Brand", "brand", "")
	if err != nil {
		t.Fatal(err)
	}
	return &designSessionServer{shellE2E: e, agent: ag.ID, ds: ds.ID}
}

// raw posts to the API and answers the status and the decoded body.
func (s *designSessionServer) raw(verb string, body map[string]any) (int, map[string]any) {
	s.t.Helper()
	b, _ := json.Marshal(body)
	status, text := postJSON(s.t, s.srv.URL+"/api/"+verb, string(b))
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return status, out
}

func (s *designSessionServer) start(prompt string) string {
	s.t.Helper()
	var out struct {
		SessionID string `json:"session_id"`
	}
	s.post("start_design_session", map[string]any{"design_system_id": s.ds, "agent_id": s.agent, "prompt": prompt}, &out)
	if out.SessionID == "" {
		s.t.Fatal("start_design_session answered no session id")
	}
	return out.SessionID
}

func (s *designSessionServer) running(ds string) int {
	s.t.Helper()
	var out struct {
		Running int `json:"running_design_sessions"`
	}
	s.post("get_design_system", map[string]any{"design_system_id": ds}, &out)
	return out.Running
}

func TestDesignSession_StartsOnTheProjectBoundToItsDesignSystemWithNoTask(t *testing.T) {
	s := newDesignSessionServer(t)
	id := s.start("Make the look calm.")

	sess := s.sessions.Get(id)
	if sess == nil {
		t.Fatalf("session %s is not recorded", id)
	}
	if sess.ProjectID != s.project || sess.TicketID != "" || sess.Mode != domain.SessionModeDesignSystem || sess.DesignSystemID != s.ds || sess.AgentID != s.agent {
		t.Fatalf("session = %+v", sess)
	}
	if !strings.Contains(sess.Task, "Brand") {
		t.Errorf("the session is not named after the design system: %q", sess.Task)
	}
	// It lists with the project's sessions, as a product session does.
	listed := false
	for _, other := range s.sessions.List(s.ctx, ports.SessionFilter{ProjectID: s.project}) {
		listed = listed || other.ID == id
	}
	if !listed {
		t.Error("the design session is not listed with the project's sessions")
	}
}

func TestDesignSession_RunningSessionsAreCountedPerDesignSystemUntilTheyEnd(t *testing.T) {
	s := newDesignSessionServer(t)
	other, err := s.cat.DesignSystems.CreateDesignSystem(s.ctx, s.project, "Other", "other", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.running(s.ds); got != 0 {
		t.Fatalf("running before = %d", got)
	}
	first := s.start("one")
	s.start("two") // a second one is allowed
	if got := s.running(s.ds); got != 2 {
		t.Fatalf("running = %d, want 2", got)
	}
	if got := s.running(other.ID); got != 0 {
		t.Fatalf("running on the other design system = %d", got)
	}
	var list struct {
		DesignSystems []struct {
			ID      string `json:"design_system_id"`
			Running int    `json:"running_design_sessions"`
		} `json:"design_systems"`
	}
	s.post("list_design_systems", map[string]any{"project_id": s.project}, &list)
	for _, row := range list.DesignSystems {
		if row.ID == s.ds && row.Running != 2 {
			t.Errorf("list says %d running, want 2", row.Running)
		}
	}
	if err := s.orch.Stop(s.ctx, first); err != nil {
		t.Fatal(err)
	}
	if got := s.running(s.ds); got != 1 {
		t.Fatalf("running after one ended = %d, want 1", got)
	}
}

func TestDesignSession_RefusesAnUnknownDesignSystemOrAgent(t *testing.T) {
	s := newDesignSessionServer(t)
	for name, body := range map[string]map[string]any{
		"design system": {"design_system_id": "nope", "agent_id": s.agent, "prompt": "x"},
		"agent":         {"design_system_id": s.ds, "agent_id": "nope", "prompt": "x"},
	} {
		status, out := s.raw("start_design_session", body)
		want := map[string]string{"design system": "DESIGN_SYSTEM_NOT_FOUND", "agent": "AGENT_NOT_FOUND"}[name]
		if status != http.StatusNotFound || out["code"] != want {
			t.Errorf("unknown %s: %d %v, want 404 %s", name, status, out, want)
		}
	}
	if got := s.running(s.ds); got != 0 {
		t.Fatalf("a refused start left %d running", got)
	}
}
