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
