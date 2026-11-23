package httpapi

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Corners of the mode agents API the first tests leave out (ADR 0103).

// In All the list and the flag are ignored, so a name that is not an agent is
// not an UNKNOWN_REFERENCE either.
func TestModeAgents_AllIgnoresTheListEvenWhenItNamesNoAgent(t *testing.T) {
	f := catalogWithSkills(t)
	got := f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "all", "agents": []string{"ghost"}, "no_agent": false}, 200)
	if got["state"] != "all" || got["no_agent"] != true || len(strList(got["agents"])) != 0 {
		t.Fatalf("set = %v", got)
	}
}

// A Selected list keeps the order it was sent in, once per name.
func TestModeAgents_ASelectedListKeepsItsOrderAndDropsDuplicates(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "alpha", "description": "d"}}, 200)
	setMode(f, "qa", "selected", false, "zeta", "alpha", "zeta", "alpha")
	if got := modeAgents(t, f, "qa"); !reflect.DeepEqual(got, []string{"zeta", "alpha"}) {
		t.Fatalf("qa = %v", got)
	}
}

// A refused write changes nothing and publishes nothing.
func TestModeAgents_ARefusedWritePublishesNothing(t *testing.T) {
	f := catalogWithSkills(t)
	events := f.followEvents()
	f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "selected", "agents": []string{"ghost"}}, 400)
	f.post("/api/set_mode_agents", map[string]any{"mode": "nope", "state": "all"}, 400)
	f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "bogus"}, 400)
	select {
	case got := <-events:
		t.Fatalf("a refused write published %s", got)
	case <-time.After(300 * time.Millisecond):
	}
}

// Mode values are the ones the contract lists: "default" is not a mode value.
func TestModeAgents_TheWordDefaultIsNotAModeValue(t *testing.T) {
	f := catalogWithSkills(t)
	got := f.post("/api/set_mode_agents", map[string]any{"mode": "default", "state": "selected", "agents": []string{}}, 400)
	if got["code"] != "INVALID_INPUT" || got["field"] != "mode" {
		t.Fatalf("body = %v", got)
	}
}

// A missing mode is the default mode; a missing state is refused naming state.
func TestModeAgents_AMissingStateIsRefusedNamingTheField(t *testing.T) {
	f := catalogWithSkills(t)
	got := f.post("/api/set_mode_agents", map[string]any{"mode": "review"}, 400)
	if got["code"] != "INVALID_INPUT" || got["field"] != "state" {
		t.Fatalf("body = %v", got)
	}
}

// Writing one mode never moves another, and re-reading after each write holds.
func TestModeAgents_EachModeKeepsItsOwnList(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "alpha", "description": "d"}}, 200)
	setMode(f, "review", "selected", false, "zeta")
	setMode(f, "test", "selected", true, "alpha")
	setMode(f, "", "selected", false)
	if got := modeAgents(t, f, "review"); !reflect.DeepEqual(got, []string{"zeta"}) {
		t.Fatalf("review = %v", got)
	}
	if e := modeEntry(t, f, "test"); !reflect.DeepEqual(strList(e["agents"]), []string{"alpha"}) || e["no_agent"] != true {
		t.Fatalf("test = %v", e)
	}
	if e := modeEntry(t, f, ""); e["state"] != "selected" || len(strList(e["agents"])) != 0 {
		t.Fatalf("default = %v", e)
	}
	for _, m := range []string{"architect", "implement", "design", "product", "designsystem", "qa", "operate"} {
		if e := modeEntry(t, f, m); e["state"] != "all" {
			t.Fatalf("%s = %v", m, e)
		}
	}
}

// An agent created later is available in every All mode, and in a Selected
// one only after a save that names it.
func TestAgentSave_ANewAgentWithModesJoinsExactlyThoseSelectedModes(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false)
	setMode(f, "qa", "selected", false)
	f.post("/api/save_catalog_agent", map[string]any{"modes": []string{"qa", "qa"}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	if got := modeAgents(t, f, "qa"); !reflect.DeepEqual(got, []string{"fresh"}) {
		t.Fatalf("qa = %v", got)
	}
	if got := modeAgents(t, f, "review"); len(got) != 0 {
		t.Fatalf("review = %v", got)
	}
	for _, m := range availableIn(t, f, "fresh") {
		if m == "review" {
			t.Fatal("fresh is available in review")
		}
	}
}

// A rename and a modes list in one save: the lists end holding the new name.
func TestAgentSave_ARenameWithModesLeavesTheNewNameInTheNamedModesOnly(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false, "zeta")
	setMode(f, "qa", "selected", false)
	f.post("/api/save_catalog_agent", map[string]any{"previous_name": "zeta", "modes": []string{"qa"}, "agent": map[string]any{"name": "omega", "description": "d", "entries": []string{"tdd"}}}, 200)
	if got := modeAgents(t, f, "qa"); !reflect.DeepEqual(got, []string{"omega"}) {
		t.Fatalf("qa = %v", got)
	}
	if got := modeAgents(t, f, "review"); len(got) != 0 {
		t.Fatalf("review = %v", got)
	}
}

// A rename that touches mode lists publishes their paths; a delete of an
// agent no list names publishes none.
func TestAgentRenameAndDelete_PublishOnlyTheModePathsTheyTouched(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false, "zeta")
	setMode(f, "qa", "selected", false)
	events := f.followEvents()
	f.post("/api/save_catalog_agent", map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "omega", "description": "d", "entries": []string{"tdd"}}}, 200)
	got := events.next(t)
	if !strings.Contains(got, `"modes/review"`) || strings.Contains(got, `"modes/qa"`) {
		t.Fatalf("event = %s", got)
	}
}

// Deleting an agent no mode list names changes no list and reports no mode change.
func TestAgentDelete_AnAgentInNoListReportsNoModeChange(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	setMode(f, "review", "selected", true, "zeta")
	del := f.post("/api/delete_catalog_agent", map[string]any{"name": "fresh"}, 200)
	if mc, _ := del["change"].(map[string]any)["modes"].([]any); len(mc) != 0 {
		t.Fatalf("change.modes = %v", mc)
	}
	if got := modeAgents(t, f, "review"); !reflect.DeepEqual(got, []string{"zeta"}) {
		t.Fatalf("review = %v", got)
	}
}

// Undoing a rename that left a Selected list empty keeps No agent as it was.
func TestAgentRename_UndoRestoresNoAgentAndTheOtherNames(t *testing.T) {
	f := catalogWithSkills(t)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "alpha", "description": "d"}}, 200)
	setMode(f, "review", "selected", false, "alpha", "zeta")
	out := f.post("/api/save_catalog_agent", map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "omega", "description": "d", "entries": []string{"tdd"}}}, 200)
	if got := modeAgents(t, f, "review"); !reflect.DeepEqual(got, []string{"alpha", "omega"}) {
		t.Fatalf("after rename review = %v", got)
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": out["change"].(map[string]any)["id"]}, 200)
	e := modeEntry(t, f, "review")
	if !reflect.DeepEqual(strList(e["agents"]), []string{"alpha", "zeta"}) || e["no_agent"] != false || e["state"] != "selected" {
		t.Fatalf("after undo review = %v", e)
	}
}

// Nothing set: every agent is available in every mode, in the order of §1.
func TestModeAgents_NothingSetEveryAgentIsAvailableInAllTenModes(t *testing.T) {
	f := catalogWithSkills(t)
	agents := list(f.get("/api/get_agent_catalog", 200)["agents"])
	if len(agents) == 0 {
		t.Fatal("no agents in the fixture")
	}
	for _, a := range agents {
		got := strList(a.(map[string]any)["available_in_modes"])
		if !reflect.DeepEqual(got, modeValues) {
			t.Fatalf("%v available in %v", a.(map[string]any)["name"], got)
		}
	}
}
