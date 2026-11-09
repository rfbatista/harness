package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// get_run_variables (ADR 0077): the person reads the variables the current or
// last run was given, each with the source that won.

func (f *servicesFixture) variables(id string) []map[string]any {
	f.t.Helper()
	code, out := f.call("GET", "/api/get_run_variables?service_id="+id, nil)
	if code != 200 {
		f.t.Fatalf("get_run_variables = %d %v", code, out)
	}
	var list []map[string]any
	for _, v := range out["variables"].([]any) {
		list = append(list, v.(map[string]any))
	}
	return list
}

func TestServicesRunVariables_EachNameOnceWithTheWinningSourceAndWhoSetAnOverride(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"C": "c-override", "D": "d-override"}})
	f.awaitLog(f.id, "got A=a-dotenv")

	code, out := f.call("GET", "/api/get_run_variables?service_id="+f.id, nil)
	if code != 200 || out["run_id"] != f.service(f.id)["run"].(map[string]any)["id"] {
		t.Fatalf("answer = %d %v", code, out)
	}
	want := []map[string]string{
		{"name": "A", "value": "a-dotenv", "source": "repository_env", "by": ""},
		{"name": "B", "value": "b-service", "source": "service", "by": ""},
		{"name": "C", "value": "c-override", "source": "override", "by": "dev · session sess-1"},
		{"name": "D", "value": "d-override", "source": "override", "by": "dev · session sess-1"},
		{"name": "S", "value": secretService, "source": "service", "by": ""},
		{"name": "T", "value": secretBoth, "source": "service", "by": ""},
	}
	got := out["variables"].([]any)
	if len(got) != len(want) {
		t.Fatalf("variables = %v", got)
	}
	for i, w := range want {
		v := got[i].(map[string]any)
		for k, wv := range w {
			if v[k] != wv {
				t.Fatalf("variable %d = %v, want %v", i, v, w)
			}
		}
	}
}

func TestServicesRunVariables_TheBaseEnvironmentIsNeverListed(t *testing.T) {
	f := newOverridesFixture(t)
	t.Setenv("HARNESS_TEST_ONLY_IN_SERVER", "x")
	f.as("sess-1").ok("start_service", named("web"))
	f.awaitLog(f.id, "got A=a-dotenv")
	for _, v := range f.variables(f.id) {
		switch v["name"] {
		case "PATH", "HOME", "USER", "SHELL", "LANG", "TMPDIR", "HARNESS_TEST_ONLY_IN_SERVER":
			t.Fatalf("the base environment is listed: %v", v)
		}
	}
}

func TestServicesRunVariables_AnOverrideOfABaseNameIsListed(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"HOME": "/elsewhere"}})
	var found bool
	for _, v := range f.variables(f.id) {
		if v["name"] == "HOME" {
			found = v["value"] == "/elsewhere" && v["source"] == "override"
		}
	}
	if !found {
		t.Fatalf("an override of HOME is a variable of the run: %v", f.variables(f.id))
	}
}

func TestServicesRunVariables_AServiceThatNeverRanAnswersNothingAndAnUnknownOneIsNotFound(t *testing.T) {
	f := newOverridesFixture(t)
	code, out := f.call("GET", "/api/get_run_variables?service_id="+f.id, nil)
	vars, ok := out["variables"].([]any)
	if code != 200 || out["run_id"] != "" || !ok || len(vars) != 0 {
		t.Fatalf("never ran = %d %v", code, out)
	}
	code, out = f.call("GET", "/api/get_run_variables?service_id=nope", nil)
	if c, _ := errorOf(out); code != 404 || c != "SERVICE_NOT_FOUND" {
		t.Fatalf("unknown = %d %v", code, out)
	}
}

func TestServicesRunVariables_APersonsFreshStartCarriesNoOverride(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "agent-value"}})
	f.operate("stop_service", f.id)
	f.operate("start_service", f.id)
	f.awaitStatus(f.id, "running")
	for _, v := range f.variables(f.id) {
		if v["source"] == "override" {
			t.Fatalf("a fresh start has an override: %v", v)
		}
	}
}

func TestServicesRunVariables_ValuesAreInNoFeedEventAndNoRunShape(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "agent-value"}})
	f.awaitLog(f.id, "got A=a-dotenv")
	_, listed := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	_, got := f.call("GET", "/api/get_service?service_id="+f.id, nil)
	b, _ := json.Marshal([]any{listed, got})
	// The service's own env is the person's to read in the form: only the
	// run's variables must not add values to the Run shape.
	run, _ := json.Marshal(f.service(f.id)["run"])
	for _, secret := range []string{"a-dotenv", "agent-value", secretDotenv, secretBoth} {
		if strings.Contains(string(run), secret) {
			t.Fatalf("the Run shape holds %q: %s", secret, run)
		}
	}
	if strings.Contains(string(b), "agent-value") || strings.Contains(string(b), secretDotenv) {
		t.Fatalf("a list answer holds a run variable value: %s", b)
	}
}
