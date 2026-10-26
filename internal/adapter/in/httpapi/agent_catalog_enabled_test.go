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

	for _, p := range list(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], "zeta")["enabled_in"]) {
		m := p.(map[string]any)
		want := map[string]string{all: "all", sel: "selected"}[m["id"].(string)]
		if m["mode"] != want || m["name"] == "" {
			t.Fatalf("enabled_in entry = %v, want mode %s", m, want)
		}
	}
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

func projectAgents(f *catalogFixture, id string) []string {
	return strList(f.get("/api/get_project_agents?project_id="+id, 200)["agents"])
}

func selectProject(f *catalogFixture, id string, agents ...string) {
	if agents == nil {
		agents = []string{}
	}
	f.post("/api/set_project_agents", map[string]any{"project_id": id, "mode": "selected", "agents": agents}, 200)
}

func TestAgentSave_ProjectsIsTheFullSetOfSelectedProjectsThatListIt(t *testing.T) {
	f := catalogWithSkills(t)
	a, b := f.project("A"), f.project("B")
	selectProject(f, a, "zeta")
	selectProject(f, b)
	body := func(projects any) map[string]any {
		m := map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "zeta", "description": "d", "entries": []string{"tdd"}}}
		if projects != nil {
			m["projects"] = projects
		}
		return m
	}
	// Absent: nothing changes.
	f.post("/api/save_catalog_agent", body(nil), 200)
	if !reflect.DeepEqual(projectAgents(f, a), []string{"zeta"}) || len(projectAgents(f, b)) != 0 {
		t.Fatalf("absent projects changed the lists: %v %v", projectAgents(f, a), projectAgents(f, b))
	}
	// [b]: added to b, removed from a.
	out := f.post("/api/save_catalog_agent", body([]string{b}), 200)
	if len(projectAgents(f, a)) != 0 || !reflect.DeepEqual(projectAgents(f, b), []string{"zeta"}) {
		t.Fatalf("lists = %v %v", projectAgents(f, a), projectAgents(f, b))
	}
	if pc := list(out["change"].(map[string]any)["projects"]); len(pc) != 2 {
		t.Fatalf("change.projects = %v", pc)
	}
	// []: removed everywhere.
	f.post("/api/save_catalog_agent", body([]string{}), 200)
	if len(projectAgents(f, b)) != 0 {
		t.Fatal("not removed from b")
	}
}

func TestAgentSave_NamingAnAllProjectIsRefused(t *testing.T) {
	f := catalogWithSkills(t)
	all := f.project("All")
	before := f.read("agents.json")
	got := f.post("/api/save_catalog_agent", map[string]any{"projects": []string{all}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 400)
	if got["code"] != "INVALID_INPUT" || got["field"] != "projects" {
		t.Fatalf("body = %v", got)
	}
	if f.read("agents.json") != before {
		t.Fatal("a refused save wrote the file")
	}
	bad := f.post("/api/save_catalog_agent", map[string]any{"projects": []string{"ghost"}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 400)
	if bad["code"] != "UNKNOWN_REFERENCE" {
		t.Fatalf("body = %v", bad)
	}
}

func TestAgentRenameAndDelete_RewriteTheSelectedListsAndUndoRestoresThem(t *testing.T) {
	f := catalogWithSkills(t)
	a, b := f.project("A"), f.project("B")
	selectProject(f, a, "zeta")
	selectProject(f, b)

	out := f.post("/api/save_catalog_agent", map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "omega", "description": "d", "entries": []string{"tdd"}}}, 200)
	if !reflect.DeepEqual(projectAgents(f, a), []string{"omega"}) {
		t.Fatalf("a = %v", projectAgents(f, a))
	}
	pc := list(out["change"].(map[string]any)["projects"])
	if len(pc) != 1 || !reflect.DeepEqual(strList(pc[0].(map[string]any)["enabled_agents_before"]), []string{"zeta"}) {
		t.Fatalf("change.projects = %v", pc)
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": out["change"].(map[string]any)["id"]}, 200)
	if !reflect.DeepEqual(projectAgents(f, a), []string{"zeta"}) {
		t.Fatalf("undo: a = %v", projectAgents(f, a))
	}

	del := f.post("/api/delete_catalog_agent", map[string]any{"name": "zeta"}, 200)
	if len(projectAgents(f, a)) != 0 {
		t.Fatalf("delete: a = %v", projectAgents(f, a))
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": del["change"].(map[string]any)["id"]}, 200)
	if !reflect.DeepEqual(projectAgents(f, a), []string{"zeta"}) {
		t.Fatalf("undo delete: a = %v", projectAgents(f, a))
	}
}

func TestAgentSave_DryRunShowsTheProjectChangeAndWritesNothing(t *testing.T) {
	f := catalogWithSkills(t)
	a := f.project("A")
	selectProject(f, a)
	out := f.post("/api/save_catalog_agent", map[string]any{"dry_run": true, "projects": []string{a}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	pc := list(out["change"].(map[string]any)["projects"])
	if len(pc) != 1 || !reflect.DeepEqual(strList(pc[0].(map[string]any)["enabled_agents_after"]), []string{"fresh"}) {
		t.Fatalf("change.projects = %v", pc)
	}
	if len(projectAgents(f, a)) != 0 {
		t.Fatal("a dry run wrote")
	}
}
