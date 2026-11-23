//go:build !windows

package web_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/ports"
)

// The top bar API against the user stories of "Spec: Harness server — React
// top bar island": each app-wide link is linked only when its page is served,
// New project stands in for Projects, an unknown or malformed path answers as
// the contract says.

type topBarCredentials struct{ ports.GitCredentials }
type topBarSavers struct{ ports.TokenSaverInstallReader }

// everyPageServed serves every page the bar can link.
func everyPageServed(d *web.Deps) {
	d.GitCredentials = topBarCredentials{}
	d.TokenSaverInstalls = topBarSavers{}
}

func TestTheFullBarLinksEveryServedPageInTheBarsOrder(t *testing.T) {
	w := newTopBarWorld(t, everyPageServed, false)
	_, body := w.get(t, "/")
	want := []string{"board", "projects", "agents", "credentials", "token_savers", "models", "agent_isolation", "server"}
	if got := linkKeys(body); !slices.Equal(got, want) {
		t.Fatalf("links = %v, want %v", got, want)
	}
}

func TestEachAppWideLinkIsHiddenWhenItsPageIsNotServed(t *testing.T) {
	cases := map[string]struct {
		hide func(*web.Deps)
		key  string
	}{
		"board":         {func(d *web.Deps) { d.AgentBoard = nil }, "board"},
		"credentials":   {func(d *web.Deps) { d.GitCredentials = nil }, "credentials"},
		"token savers":  {func(d *web.Deps) { d.TokenSaverInstalls = nil }, "token_savers"},
		"models":        {func(d *web.Deps) { d.Settings = nil }, "models"},
		"isolation":     {func(d *web.Deps) { d.SandboxSettings = nil }, "agent_isolation"},
		"server":        {func(d *web.Deps) { d.DataPaths = nil }, "server"},
		"projects list": {func(d *web.Deps) { d.ProjectSummaries = nil }, "projects"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newTopBarWorld(t, func(d *web.Deps) { everyPageServed(d); c.hide(d) }, false)
			_, body := w.get(t, "/")
			keys := linkKeys(body)
			if slices.Contains(keys, c.key) {
				t.Errorf("%s is linked though its page is not served: %v", c.key, keys)
			}
			if len(keys) < 6 {
				t.Errorf("hiding %s must hide only it, got %v", c.key, keys)
			}
		})
	}
}

func TestAgentsIsAlwaysLinkedAndNewProjectNeverSitsBesideProjects(t *testing.T) {
	bare := newTopBarWorld(t, func(d *web.Deps) {
		d.AgentBoard, d.DataPaths, d.Settings, d.SandboxSettings, d.ProjectSummaries, d.Notifications = nil, nil, nil, nil, nil, nil
	}, false)
	_, body := bare.get(t, "/")
	if got := linkKeys(body); !slices.Equal(got, []string{"new_project", "agents"}) {
		t.Fatalf("links = %v, want [new_project agents]", got)
	}
	if body["notifications"] != nil {
		t.Fatalf("notifications = %v, want null", body["notifications"])
	}
	for _, served := range []bool{true, false} {
		w := newTopBarWorld(t, func(d *web.Deps) {
			if !served {
				d.ProjectSummaries = nil
			}
		}, false)
		_, body := w.get(t, "/")
		keys := linkKeys(body)
		if slices.Contains(keys, "projects") && slices.Contains(keys, "new_project") {
			t.Errorf("served=%v: projects and new_project both appear: %v", served, keys)
		}
		if slices.Contains(keys, "projects") == slices.Contains(keys, "new_project") {
			t.Errorf("served=%v: exactly one of projects and new_project must appear: %v", served, keys)
		}
	}
}

func TestNoProjectsMeansAnEmptyPickerListAndNoCurrentProject(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	*w.projects = nil
	for _, path := range []string{"/", "/projects/p1", "/projects/p1/tasks/t1"} {
		status, body := w.get(t, path)
		if status != 200 || body["current_project_id"] != "" {
			t.Errorf("%s = %d, current project %v, want none", path, status, body["current_project_id"])
		}
		if list, ok := body["projects"].([]any); !ok || len(list) != 0 {
			t.Errorf("%s: projects = %v, want []", path, body["projects"])
		}
	}
}

func TestAPathIsMatchedWholeNotByPrefix(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	for _, path := range []string{"/agentsx", "/serverless", "/modelsx", "/projectsx", "/agent-isolationx"} {
		status, body := w.get(t, path)
		if status != 200 || len(currentKeys(body)) != 0 {
			t.Errorf("%s = %d, current %v, want none", path, status, currentKeys(body))
		}
	}
}

func TestAnEncodedProjectIdInThePathIsTheCurrentProject(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	status, body := w.get(t, "/projects/p%31/tasks/t1")
	if status != 200 || body["current_project_id"] != "p1" {
		t.Fatalf("= %d %v, want current project p1", status, body["current_project_id"])
	}
}

func TestAnUnknownPathAnswersTheWholeBarWithNoCurrentMark(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	status, body := w.get(t, "/no/such/page")
	if status != 200 || body["path"] != "/no/such/page" {
		t.Fatalf("= %d %v", status, body)
	}
	if len(linkKeys(body)) == 0 || len(currentKeys(body)) != 0 || body["notifications"] == nil {
		t.Fatalf("an unknown path still gets the bar, current %v: %v", currentKeys(body), body)
	}
}

func TestMalformedPathsAreRefusedWhatEverTheWayTheyAreWritten(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	raw := []string{
		"/api/top_bar?path=%2Fa%23b",      // fragment
		"/api/top_bar?path=%2Fa%3Fb%3Dc",  // query
		"/api/top_bar?path=%2Fa%5Cb",      // backslash
		"/api/top_bar?path=%2Fa%0Ab",      // newline
		"/api/top_bar?path=%2Fa%09b",      // tab
		"/api/top_bar?path=%2Fa%7Fb",      // DEL
		"/api/top_bar?path=%20%2Fa",       // leading space
		"/api/top_bar?path=javascript:1",  // scheme
		"/api/top_bar?path=%2F%2Fhost%2F", // network path
		"/api/top_bar?path=%2Fa%25zz",     // bad escape
		"/api/top_bar?other=%2F",          // wrong parameter
	}
	for _, r := range raw {
		res, err := http.Get(w.srv.URL + r)
		status, body := decodeTopBar(t, res, err)
		if status != 400 || body["code"] != "INVALID_INPUT" {
			t.Errorf("%s = %d %v, want 400 INVALID_INPUT", r, status, body)
		}
	}
}

func TestWellFormedPathsWithOddCharactersAreNotRefused(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	for _, path := range []string{"/projects/p1/tasks/t%201", "/a//b", "/projects/p1/", "/café", "/a;b", "/a=b&c"} {
		res, err := http.Get(w.srv.URL + "/api/top_bar?path=" + url.QueryEscape(path))
		if status, body := decodeTopBar(t, res, err); status != 200 {
			t.Errorf("%q = %d %v, want 200", path, status, body)
		}
	}
}

func TestTheEndpointIsGetOnly(t *testing.T) {
	w := newTopBarWorld(t, nil, false)
	res, err := http.Post(w.srv.URL+"/api/top_bar?path=%2F", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed && res.StatusCode != http.StatusNotFound {
		t.Errorf("POST = %d, want 404 or 405", res.StatusCode)
	}
}
