package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

var modeValues = []string{"", "architect", "implement", "design", "product", "designsystem", "review", "test", "qa", "operate"}

func modeEntries(t *testing.T, f *catalogFixture) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range list(f.get("/api/get_mode_agents", 200)["modes"]) {
		out = append(out, e.(map[string]any))
	}
	return out
}

func modeEntry(t *testing.T, f *catalogFixture, mode string) map[string]any {
	t.Helper()
	for _, e := range modeEntries(t, f) {
		if e["mode"] == mode {
			return e
		}
	}
	t.Fatalf("no entry for mode %q", mode)
	return nil
}

func setMode(f *catalogFixture, mode, state string, noAgent bool, agents ...string) {
	if agents == nil {
		agents = []string{}
	}
	f.post("/api/set_mode_agents", map[string]any{"mode": mode, "state": state, "agents": agents, "no_agent": noAgent}, 200)
}

func TestModeAgents_EveryModeStartsInAllAgentsInTheOrderOfTheModes(t *testing.T) {
	f := catalogWithSkills(t)
	entries := modeEntries(t, f)
	if len(entries) != len(modeValues) {
		t.Fatalf("got %d entries, want %d", len(entries), len(modeValues))
	}
	for i, e := range entries {
		if e["mode"] != modeValues[i] || e["name"] == "" || e["state"] != "all" || len(strList(e["agents"])) != 0 || e["no_agent"] != true {
			t.Fatalf("entry %d = %v", i, e)
		}
	}
}

func TestModeAgents_SetAndGetRoundTrip(t *testing.T) {
	f := catalogWithSkills(t)
	got := f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "selected", "agents": []string{"zeta", "zeta"}, "no_agent": false}, 200)
	if got["mode"] != "review" || got["state"] != "selected" || got["no_agent"] != false || !reflect.DeepEqual(strList(got["agents"]), []string{"zeta"}) {
		t.Fatalf("set = %v", got)
	}
	if e := modeEntry(t, f, "review"); !reflect.DeepEqual(e, got) {
		t.Fatalf("get = %v, set answered %v", e, got)
	}
	// The other modes stay as they were.
	if e := modeEntry(t, f, "qa"); e["state"] != "all" {
		t.Fatalf("qa = %v", e)
	}

	// No agent is an entry of the list.
	setMode(f, "review", "selected", true, "zeta")
	if e := modeEntry(t, f, "review"); e["no_agent"] != true {
		t.Fatalf("review = %v", e)
	}

	// An empty list closes the mode.
	setMode(f, "review", "selected", false)
	if e := modeEntry(t, f, "review"); e["state"] != "selected" || len(strList(e["agents"])) != 0 || e["no_agent"] != false {
		t.Fatalf("closed review = %v", e)
	}

	// All ignores the list and the flag.
	setMode(f, "review", "all", false, "zeta")
	if e := modeEntry(t, f, "review"); e["state"] != "all" || len(strList(e["agents"])) != 0 || e["no_agent"] != true {
		t.Fatalf("reopened review = %v", e)
	}
}

func TestModeAgents_TheDefaultModeIsTheEmptyModeValue(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "", "selected", false, "zeta")
	if e := modeEntry(t, f, ""); e["state"] != "selected" || !reflect.DeepEqual(strList(e["agents"]), []string{"zeta"}) {
		t.Fatalf("default = %v", e)
	}
}

func TestModeAgents_Refusals(t *testing.T) {
	f := catalogWithSkills(t)
	if got := f.post("/api/set_mode_agents", map[string]any{"mode": "nope", "state": "all"}, 400); got["code"] != "INVALID_INPUT" || got["field"] != "mode" {
		t.Fatalf("body = %v", got)
	}
	if got := f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "some"}, 400); got["code"] != "INVALID_INPUT" || got["field"] != "state" {
		t.Fatalf("body = %v", got)
	}
	bad := f.post("/api/set_mode_agents", map[string]any{"mode": "review", "state": "selected", "agents": []string{"zeta", "ghost"}}, 400)
	if bad["code"] != "UNKNOWN_REFERENCE" || !reflect.DeepEqual(strList(bad["names"]), []string{"ghost"}) {
		t.Fatalf("body = %v", bad)
	}
	if e := modeEntry(t, f, "review"); e["state"] != "all" {
		t.Fatalf("a refused write changed the mode: %v", e)
	}
}

func TestModeAgents_ASettingSurvivesARestartAndIsServerWide(t *testing.T) {
	f := catalogWithSkills(t)
	f.project("One")
	setMode(f, "qa", "selected", true, "zeta")
	f.restart()
	if e := modeEntry(t, f, "qa"); e["state"] != "selected" || e["no_agent"] != true || !reflect.DeepEqual(strList(e["agents"]), []string{"zeta"}) {
		t.Fatalf("qa after restart = %v", e)
	}
}

func TestModeAgents_AWritePublishesTheModesPath(t *testing.T) {
	f := catalogWithSkills(t)
	events := f.followEvents()
	setMode(f, "review", "selected", false, "zeta")
	if got := events.next(t); got != `{"agent_catalog_changed":{"paths":["modes/review"]}}` {
		t.Fatalf("event = %s", got)
	}
	setMode(f, "", "all", false)
	if got := events.next(t); got != `{"agent_catalog_changed":{"paths":["modes/default"]}}` {
		t.Fatalf("event = %s", got)
	}
}

// catalogEventLines are the data lines of the project event stream.
type catalogEventLines chan string

func (l catalogEventLines) next(t *testing.T) string {
	t.Helper()
	select {
	case got, ok := <-l:
		if !ok {
			t.Fatal("the event stream closed")
		}
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	return ""
}

// followEvents opens /api/project_events on the fixture's server.
func (f *catalogFixture) followEvents() catalogEventLines {
	f.t.Helper()
	srv := httptest.NewServer(f.router)
	f.t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/project_events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { resp.Body.Close() })
	lines := make(catalogEventLines)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				select {
				case lines <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	// The stream is open once its headers came; give the follow a moment.
	time.Sleep(100 * time.Millisecond)
	return lines
}

func availableIn(t *testing.T, f *catalogFixture, agent string) []string {
	t.Helper()
	return strList(byName(t, f.get("/api/get_agent_catalog", 200)["agents"], agent)["available_in_modes"])
}

func modeAgents(t *testing.T, f *catalogFixture, mode string) []string {
	t.Helper()
	return strList(modeEntry(t, f, mode)["agents"])
}

func TestModeAgents_CatalogReadHoldsEachAllModeAndTheSelectedModesThatListTheAgent(t *testing.T) {
	f := catalogWithSkills(t)
	if got := availableIn(t, f, "zeta"); !reflect.DeepEqual(got, modeValues) {
		t.Fatalf("zeta available in %v, want every mode", got)
	}
	setMode(f, "review", "selected", false, "zeta")
	setMode(f, "qa", "selected", true)
	got := availableIn(t, f, "zeta")
	want := []string{"", "architect", "implement", "design", "product", "designsystem", "review", "test", "operate"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zeta available in %v, want %v", got, want)
	}

	// An agent created later is in the All modes only.
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	want = []string{"", "architect", "implement", "design", "product", "designsystem", "test", "operate"}
	if got := availableIn(t, f, "fresh"); !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh available in %v, want %v", got, want)
	}
}

func TestAgentSave_ModesIsTheFullSetOfSelectedModesThatListIt(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false, "zeta")
	setMode(f, "qa", "selected", false)
	body := func(modes any) map[string]any {
		m := map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "zeta", "description": "d", "entries": []string{"tdd"}}}
		if modes != nil {
			m["modes"] = modes
		}
		return m
	}
	// Absent: nothing changes.
	f.post("/api/save_catalog_agent", body(nil), 200)
	if !reflect.DeepEqual(modeAgents(t, f, "review"), []string{"zeta"}) || len(modeAgents(t, f, "qa")) != 0 {
		t.Fatalf("absent modes changed the lists: %v %v", modeAgents(t, f, "review"), modeAgents(t, f, "qa"))
	}
	// [qa]: added to qa, removed from review.
	out := f.post("/api/save_catalog_agent", body([]string{"qa"}), 200)
	if len(modeAgents(t, f, "review")) != 0 || !reflect.DeepEqual(modeAgents(t, f, "qa"), []string{"zeta"}) {
		t.Fatalf("lists = %v %v", modeAgents(t, f, "review"), modeAgents(t, f, "qa"))
	}
	mc := list(out["change"].(map[string]any)["modes"])
	if len(mc) != 2 {
		t.Fatalf("change.modes = %v", mc)
	}
	// [] removes it everywhere.
	f.post("/api/save_catalog_agent", body([]string{}), 200)
	if len(modeAgents(t, f, "qa")) != 0 {
		t.Fatal("not removed from qa")
	}
}

func TestAgentSave_NamingAnAllModeIsRefused(t *testing.T) {
	f := catalogWithSkills(t)
	before := f.read("agents.json")
	got := f.post("/api/save_catalog_agent", map[string]any{"modes": []string{"review"}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 400)
	if got["code"] != "INVALID_INPUT" || got["field"] != "modes" {
		t.Fatalf("body = %v", got)
	}
	if f.read("agents.json") != before {
		t.Fatal("a refused save wrote the file")
	}
	if bad := f.post("/api/save_catalog_agent", map[string]any{"modes": []string{"nope"}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 400); bad["field"] != "modes" {
		t.Fatalf("body = %v", bad)
	}
}

func TestAgentRenameAndDelete_RewriteTheModeListsAndUndoRestoresThem(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", true, "zeta")
	setMode(f, "qa", "selected", false)

	out := f.post("/api/save_catalog_agent", map[string]any{"previous_name": "zeta", "agent": map[string]any{"name": "omega", "description": "d", "entries": []string{"tdd"}}}, 200)
	if !reflect.DeepEqual(modeAgents(t, f, "review"), []string{"omega"}) {
		t.Fatalf("review = %v", modeAgents(t, f, "review"))
	}
	mc := list(out["change"].(map[string]any)["modes"])
	if len(mc) != 1 {
		t.Fatalf("change.modes = %v", mc)
	}
	m := mc[0].(map[string]any)
	if m["mode"] != "review" || m["name"] == "" || !reflect.DeepEqual(strList(m["before"]), []string{"zeta"}) || !reflect.DeepEqual(strList(m["after"]), []string{"omega"}) {
		t.Fatalf("change.modes[0] = %v", m)
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": out["change"].(map[string]any)["id"]}, 200)
	if !reflect.DeepEqual(modeAgents(t, f, "review"), []string{"zeta"}) || modeEntry(t, f, "review")["no_agent"] != true {
		t.Fatalf("undo: review = %v", modeEntry(t, f, "review"))
	}

	del := f.post("/api/delete_catalog_agent", map[string]any{"name": "zeta"}, 200)
	if len(modeAgents(t, f, "review")) != 0 {
		t.Fatalf("delete: review = %v", modeAgents(t, f, "review"))
	}
	if mc := list(del["change"].(map[string]any)["modes"]); len(mc) != 1 {
		t.Fatalf("delete change.modes = %v", mc)
	}
	f.post("/api/undo_catalog_change", map[string]any{"change_id": del["change"].(map[string]any)["id"]}, 200)
	if !reflect.DeepEqual(modeAgents(t, f, "review"), []string{"zeta"}) {
		t.Fatalf("undo delete: review = %v", modeAgents(t, f, "review"))
	}
	// An All mode is untouched by all of it.
	if e := modeEntry(t, f, "test"); e["state"] != "all" {
		t.Fatalf("test = %v", e)
	}
}

func TestAgentSave_DryRunShowsTheModeChangeAndWritesNothing(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false)
	out := f.post("/api/save_catalog_agent", map[string]any{"dry_run": true, "modes": []string{"review"}, "agent": map[string]any{"name": "fresh", "description": "d"}}, 200)
	mc := list(out["change"].(map[string]any)["modes"])
	if len(mc) != 1 || !reflect.DeepEqual(strList(mc[0].(map[string]any)["after"]), []string{"fresh"}) {
		t.Fatalf("change.modes = %v", mc)
	}
	if len(modeAgents(t, f, "review")) != 0 {
		t.Fatal("a dry run wrote the list")
	}
}

func TestAgentSave_AWriteThatTouchesModeListsPublishesTheirPaths(t *testing.T) {
	f := catalogWithSkills(t)
	setMode(f, "review", "selected", false, "zeta")
	events := f.followEvents()
	f.post("/api/delete_catalog_agent", map[string]any{"name": "zeta"}, 200)
	got := events.next(t)
	if !strings.Contains(got, `"modes/review"`) {
		t.Fatalf("event = %s", got)
	}
}

func TestModeAgents_TheGenericSettingsWriteRefusesTheirKeysAndPointsToSetModeAgents(t *testing.T) {
	f := catalogWithSkills(t)
	for _, key := range []string{"agents.mode.review", "agents.mode.default"} {
		got := f.post("/api/update_settings", map[string]any{"settings": map[string]string{key: `{"agents":["ghost"],"no_agent":true}`}}, 400)
		if got["code"] != "INVALID_INPUT" || !strings.Contains(got["error"].(string), "set_mode_agents") {
			t.Fatalf("%s: body = %v", key, got)
		}
	}
	// A clear is refused too: the write that resets the setting is set_mode_agents.
	f.post("/api/update_settings", map[string]any{"settings": map[string]string{"agents.mode.qa": ""}}, 400)
	for _, e := range modeEntries(t, f) {
		if e["state"] != "all" {
			t.Fatalf("a refused write changed %v", e)
		}
	}
	// The dedicated write still works.
	setMode(f, "review", "selected", false, "zeta")
}
