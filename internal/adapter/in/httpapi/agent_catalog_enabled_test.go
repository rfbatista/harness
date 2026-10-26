package httpapi

import (
	"reflect"
	"testing"
)

func enabledIn(t *testing.T, f *catalogFixture, agent string) []string {
	t.Helper()
	var ids []string
	for _, p := range list(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], agent)["enabled_in"]) {
		ids = append(ids, p.(map[string]any)["id"].(string))
	}
	return ids
}

func TestProjectAgents_ExistingProjectsAreInAllMode(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	got := f.get("/api/get_project_agents?project_id="+p, 200)
	if got["mode"] != "all" || len(strList(got["agents"])) != 0 {
		t.Fatalf("answer = %v", got)
	}
	f.get("/api/get_project_agents?project_id=nope", 404)
}

func TestProjectAgents_SetAndGetRoundTrip(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	got := f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"zeta", "zeta"}}, 200)
	if got["mode"] != "selected" || !reflect.DeepEqual(strList(got["agents"]), []string{"zeta"}) {
		t.Fatalf("set = %v", got)
	}
	if got := f.get("/api/get_project_agents?project_id="+p, 200); got["mode"] != "selected" || !reflect.DeepEqual(strList(got["agents"]), []string{"zeta"}) {
		t.Fatalf("get = %v", got)
	}
	// A selected project with no agent is allowed.
	got = f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{}}, 200)
	if got["mode"] != "selected" || len(strList(got["agents"])) != 0 {
		t.Fatalf("empty selected = %v", got)
	}
	// All mode stores the list empty.
	got = f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "all", "agents": []string{"zeta"}}, 200)
	if got["mode"] != "all" || len(strList(got["agents"])) != 0 {
		t.Fatalf("all = %v", got)
	}
	if got := f.get("/api/get_project_agents?project_id="+p, 200); got["mode"] != "all" || len(strList(got["agents"])) != 0 {
		t.Fatalf("get after all = %v", got)
	}
}

func TestProjectAgents_Refusals(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	bad := f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "selected", "agents": []string{"ghost"}}, 400)
	if bad["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(bad["names"]), []string{"ghost"}) {
		t.Fatalf("body = %v", bad)
	}
	if got := f.post("/api/set_project_agents", map[string]any{"project_id": p, "mode": "some"}, 400); got["code"] != "INVALID_INPUT" || got["field"] != "mode" {
		t.Fatalf("body = %v", got)
	}
	f.post("/api/set_project_agents", map[string]any{"project_id": "nope", "mode": "all"}, 404)
	if got := f.get("/api/get_project_agents?project_id="+p, 200); got["mode"] != "all" {
		t.Fatalf("a refused set changed the project: %v", got)
	}
}

func TestProjectAgents_EnabledInHoldsAllProjectsAndTheSelectedOnesThatListTheAgent(t *testing.T) {
	f := catalogWithSkills(t)
	all := f.project("All")
	sel := f.project("Selected")
	other := f.project("Other")
	f.post("/api/set_project_agents", map[string]any{"project_id": sel, "mode": "selected", "agents": []string{"zeta"}}, 200)
	f.post("/api/set_project_agents", map[string]any{"project_id": other, "mode": "selected", "agents": []string{}}, 200)

	if got := enabledIn(t, f, "zeta"); len(got) != 2 || !contains2(got, all) || !contains2(got, sel) {
		t.Fatalf("zeta enabled in %v", got)
	}
	// A new agent is in the all projects only.
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	if got := enabledIn(t, f, "fresh"); !reflect.DeepEqual(got, []string{all}) {
		t.Fatalf("fresh enabled in %v, want only %s", got, all)
	}
}

func contains2(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
