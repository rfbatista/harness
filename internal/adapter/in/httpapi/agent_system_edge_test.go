package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
)

// Seam C, edge cases: the whole contract as the settings UI sees it.

type edgeWorld struct {
	h    *Handler
	feed *feedRecorder
	svc  *agentsystem.Service
	bus  *eventbus.Bus
}

func newEdgeWorld(t *testing.T, dsn string) edgeWorld {
	t.Helper()
	db, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	feed := &feedRecorder{}
	bus := eventbus.New()
	svc := agentsystem.New(agentsystem.Deps{
		Store: sqlite.NewAgentSystemRepository(db), Feed: feed, Events: bus,
		Projects:     agentSystemProjects{"p1": {ID: "p1"}, "p2": {ID: "p2"}},
		Repositories: agentSystemRepositories{"r1": {ID: "r1", ProjectID: "p1"}, "r2": {ID: "r2", ProjectID: "p1"}, "other": {ID: "other", ProjectID: "p2"}},
		ReviewModel:  func() string { return "opus" },
	})
	svc.Subscribe(bus)
	return edgeWorld{h: NewHandler(Services{AgentSystem: svc}), feed: feed, svc: svc, bus: bus}
}

type viewBody struct {
	Effective struct {
		Guard struct {
			RetryLimit struct {
				Value  int    `json:"value"`
				Source string `json:"source"`
			} `json:"retry_limit"`
			Gates []struct {
				Kind    string `json:"kind"`
				Command struct {
					Value  string `json:"value"`
					Source string `json:"source"`
					Reason string `json:"reason"`
				} `json:"command"`
			} `json:"gates"`
		} `json:"guard"`
		Review struct {
			Critic struct {
				Model struct {
					Value  string `json:"value"`
					Source string `json:"source"`
				} `json:"model"`
			} `json:"critic"`
		} `json:"review"`
		Missing []string `json:"missing_required_gates"`
	} `json:"effective"`
	Values map[string]json.RawMessage `json:"values"`
}

func view(t *testing.T, rec *httptest.ResponseRecorder) viewBody {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var v viewBody
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func errorsOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	var e agentSystemErrors
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, x := range e.Errors {
		if x.Message == "" {
			t.Errorf("error %s has no message", x.Field)
		}
		out[x.Field] = x.Code
	}
	return out
}

func TestEveryContractCodeAnswers422WithFieldCodeAndMessageAtBothLevels(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	for _, base := range []string{"/projects/p1/agent-system", "/projects/p1/repositories/r1/agent-system"} {
		for body, want := range map[string]string{
			`{"guard.gates.test.command": ""}`:               "REQUIRED_GATE_MISSING",
			`{"guard.gates.build.enabled": false}`:           "REQUIRED_GATE_DISABLED",
			`{"guard.protected_paths": ["["]}`:               "INVALID_GLOB",
			`{"review.critic.model": "nope"}`:                "MODEL_UNAVAILABLE",
			`{"review.visual_qa.ui": {"url":"x"}}`:           "INVALID_ROUTE",
			`{"guard.retry_limit": 11}`:                      "OUT_OF_RANGE",
			`{"guard.retry_limit": "3"}`:                     "INVALID_TYPE",
			`{"guard.bogus": 1}`:                             "UNKNOWN_FIELD",
			`{"guard.gates.test.timeout_seconds": 0}`:        "OUT_OF_RANGE",
			`{"guard.gates.nope.command": "x"}`:              "UNKNOWN_FIELD",
		} {
			got := errorsOf(t, patchJSON(t, w.h, base, body))
			found := false
			for _, c := range got {
				found = found || c == want
			}
			if !found {
				t.Errorf("PATCH %s %s -> %v, want %s", base, body, got, want)
			}
		}
	}
	if len(w.feed.changes) != 0 {
		t.Errorf("refused saves were announced: %v", w.feed.changes)
	}
}

func TestOneRefusedSaveNamesEveryBadFieldAndStoresNone(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	rec := patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.retry_limit": 0, "review.critic.model":"x", "guard.gates.lint.enabled": false, "learn.context_map.enabled": false}`)
	got := errorsOf(t, rec)
	if len(got) != 3 || got["guard.retry_limit"] != "OUT_OF_RANGE" || got["review.critic.model"] != "MODEL_UNAVAILABLE" || got["guard.gates.lint.enabled"] != "REQUIRED_GATE_DISABLED" {
		t.Errorf("errors = %v", got)
	}
	if v := view(t, getJSON(t, w.h, "/projects/p1/agent-system")); len(v.Values) != 0 {
		t.Errorf("stored %v", v.Values)
	}
}

func TestMalformedBodiesAreRefusedWithoutStoringOrAnnouncing(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	for _, body := range []string{``, `not json`, `[1,2]`, `null`, `"str"`, `42`, `{"guard.retry_limit": 5`} {
		rec := patchJSON(t, w.h, "/projects/p1/agent-system", body)
		if rec.Code < 400 || rec.Code >= 500 {
			t.Errorf("body %q -> %d %s, want a 4xx", body, rec.Code, rec.Body.String())
		}
	}
	if len(w.feed.changes) != 0 {
		t.Errorf("announced %v", w.feed.changes)
	}
}

func TestPatchOfAnUnknownRepositoryOrOneOfAnotherProjectIs404(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	for _, path := range []string{"/projects/p1/repositories/nope/agent-system", "/projects/p1/repositories/other/agent-system", "/projects/nope/repositories/r1/agent-system"} {
		if rec := getJSON(t, w.h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s -> %d", path, rec.Code)
		}
		if rec := patchJSON(t, w.h, path, `{"guard.retry_limit": 5}`); rec.Code != http.StatusNotFound {
			t.Errorf("PATCH %s -> %d", path, rec.Code)
		}
	}
	if len(w.feed.changes) != 0 {
		t.Errorf("announced %v", w.feed.changes)
	}
}

func TestInvalidPatchOfAnUnknownProjectIs404NotUnprocessable(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	if rec := patchJSON(t, w.h, "/projects/nope/agent-system", `{"guard.retry_limit": 99}`); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRepositoryOverrideThenResetFollowsTheProjectDefaultOverHTTP(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.retry_limit": 5, "review.critic.model": "sonnet"}`)
	repo := "/projects/p1/repositories/r1/agent-system"

	v := view(t, getJSON(t, w.h, repo))
	if v.Effective.Guard.RetryLimit.Value != 5 || v.Effective.Guard.RetryLimit.Source != "project" || v.Effective.Review.Critic.Model.Source != "project" {
		t.Fatalf("inherited = %+v", v.Effective)
	}
	v = view(t, patchJSON(t, w.h, repo, `{"guard.retry_limit": 8}`))
	if v.Effective.Guard.RetryLimit.Value != 8 || v.Effective.Guard.RetryLimit.Source != "repository" || string(v.Values["guard.retry_limit"]) != "8" {
		t.Fatalf("override = %+v values=%v", v.Effective.Guard.RetryLimit, v.Values)
	}
	if v2 := view(t, getJSON(t, w.h, "/projects/p1/repositories/r2/agent-system")); v2.Effective.Guard.RetryLimit.Value != 5 {
		t.Errorf("the sibling saw r1's override: %+v", v2.Effective.Guard.RetryLimit)
	}
	v = view(t, patchJSON(t, w.h, repo, `{"guard.retry_limit": null}`))
	if v.Effective.Guard.RetryLimit.Value != 5 || v.Effective.Guard.RetryLimit.Source != "project" || len(v.Values) != 0 {
		t.Fatalf("after reset = %+v values=%v", v.Effective.Guard.RetryLimit, v.Values)
	}
	v = view(t, patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.retry_limit": null}`))
	if v.Effective.Guard.RetryLimit.Value != 3 || v.Effective.Guard.RetryLimit.Source != "harness" {
		t.Fatalf("after project reset = %+v", v.Effective.Guard.RetryLimit)
	}
}

func TestProjectViewValuesAreOnlyTheProjectsOwn(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	patchJSON(t, w.h, "/projects/p1/repositories/r1/agent-system", `{"guard.retry_limit": 8}`)
	v := view(t, getJSON(t, w.h, "/projects/p1/agent-system"))
	if len(v.Values) != 0 || v.Effective.Guard.RetryLimit.Source != "harness" {
		t.Errorf("project view = %+v %v", v.Effective.Guard.RetryLimit, v.Values)
	}
}

func TestEachSaveIsPushedOnceWithItsProjectAndRepository(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	patchJSON(t, w.h, "/projects/p1/repositories/r1/agent-system", `{"guard.retry_limit": 4}`)
	patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.retry_limit": null}`) // a reset is a save too
	if len(w.feed.changes) != 2 {
		t.Fatalf("changes = %+v", w.feed.changes)
	}
	a, b := w.feed.changes[0].AgentSystemSettings, w.feed.changes[1].AgentSystemSettings
	if a == nil || a.ProjectID != "p1" || a.RepositoryID != "r1" || b == nil || b.ProjectID != "p1" || b.RepositoryID != "" {
		t.Errorf("changes = %+v %+v", a, b)
	}
	raw, _ := json.Marshal(w.feed.changes[1])
	if !strings.Contains(string(raw), `"agent_system_settings"`) {
		t.Errorf("wire = %s", raw)
	}
}

func TestSavedSettingsSurviveAServerRestartOverHTTP(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "harness.db")
	first := newEdgeWorld(t, dsn)
	patchJSON(t, first.h, "/projects/p1/agent-system", `{"guard.retry_limit": 6, "guard.gates.test.command": "make test"}`)
	patchJSON(t, first.h, "/projects/p1/repositories/r1/agent-system", `{"guard.retry_limit": 9}`)

	second := newEdgeWorld(t, dsn) // a new process on the same database file
	p := view(t, getJSON(t, second.h, "/projects/p1/agent-system"))
	if p.Effective.Guard.RetryLimit.Value != 6 || string(p.Values["guard.gates.test.command"]) != `"make test"` {
		t.Errorf("project after restart = %+v %v", p.Effective.Guard.RetryLimit, p.Values)
	}
	r := view(t, getJSON(t, second.h, "/projects/p1/repositories/r1/agent-system"))
	if r.Effective.Guard.RetryLimit.Value != 9 || r.Effective.Guard.RetryLimit.Source != "repository" {
		t.Errorf("repository after restart = %+v", r.Effective.Guard.RetryLimit)
	}
	for _, g := range r.Effective.Guard.Gates {
		if g.Kind == "test" && (g.Command.Value != "make test" || g.Command.Source != "project") {
			t.Errorf("test gate after restart = %+v", g.Command)
		}
	}
}

func TestDeletingARepositoryOrProjectClearsItsSettingsOverHTTP(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	ctx := context.Background()
	patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.retry_limit": 6}`)
	patchJSON(t, w.h, "/projects/p1/repositories/r1/agent-system", `{"guard.retry_limit": 9}`)
	patchJSON(t, w.h, "/projects/p1/repositories/r2/agent-system", `{"guard.retry_limit": 2}`)

	_ = w.bus.Publish(ctx, domain.RepositoryDeleted{RepositoryID: "r1", ProjectID: "p1"})
	if v := view(t, getJSON(t, w.h, "/projects/p1/repositories/r1/agent-system")); len(v.Values) != 0 || v.Effective.Guard.RetryLimit.Value != 6 {
		t.Errorf("r1 after delete = %v %+v", v.Values, v.Effective.Guard.RetryLimit)
	}
	if v := view(t, getJSON(t, w.h, "/projects/p1/repositories/r2/agent-system")); v.Effective.Guard.RetryLimit.Value != 2 {
		t.Errorf("r2 lost its override: %+v", v.Effective.Guard.RetryLimit)
	}
	_ = w.bus.Publish(ctx, domain.ProjectDeleted{ProjectID: "p1"})
	for _, path := range []string{"/projects/p1/agent-system", "/projects/p1/repositories/r2/agent-system"} {
		if v := view(t, getJSON(t, w.h, path)); len(v.Values) != 0 || v.Effective.Guard.RetryLimit.Source != "harness" {
			t.Errorf("%s after project delete = %v %+v", path, v.Values, v.Effective.Guard.RetryLimit)
		}
	}
}

func TestAProjectDeleteLeavesAnotherProjectsSettingsAlone(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	patchJSON(t, w.h, "/projects/p2/agent-system", `{"guard.retry_limit": 7}`)
	patchJSON(t, w.h, "/projects/p2/repositories/other/agent-system", `{"guard.retry_limit": 8}`)
	_ = w.bus.Publish(context.Background(), domain.ProjectDeleted{ProjectID: "p1"})
	if v := view(t, getJSON(t, w.h, "/projects/p2/repositories/other/agent-system")); v.Effective.Guard.RetryLimit.Value != 8 {
		t.Errorf("p2's repository lost its override: %+v", v.Effective.Guard.RetryLimit)
	}
}

func TestARepositoryWithNothingDetectedIsRedAndSavedCommandsClearIt(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	repo := "/projects/p1/repositories/r1/agent-system"
	v := view(t, getJSON(t, w.h, repo))
	if len(v.Effective.Missing) != 5 {
		t.Fatalf("missing = %v, want the five required kinds", v.Effective.Missing)
	}
	patchJSON(t, w.h, "/projects/p1/agent-system", `{"guard.gates.build.command":"b","guard.gates.test.command":"t","guard.gates.lint.command":"l","guard.gates.secret_scan.command":"s","guard.gates.lockfile.command":"k"}`)
	if v = view(t, getJSON(t, w.h, repo)); len(v.Effective.Missing) != 0 {
		t.Errorf("missing = %v after the project set every command", v.Effective.Missing)
	}
	// the repository can still not blank a required gate to red
	if got := errorsOf(t, patchJSON(t, w.h, repo, `{"guard.gates.build.command":""}`)); got["guard.gates.build.command"] != "REQUIRED_GATE_MISSING" {
		t.Errorf("errors = %v", got)
	}
}

func TestTheAPIAnswersUnavailableWhenTheStoreIsNotWired(t *testing.T) {
	h := NewHandler(Services{})
	if rec := getJSON(t, h, "/projects/p1/agent-system"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET -> %d", rec.Code)
	}
	if rec := patchJSON(t, h, "/projects/p1/repositories/r1/agent-system", `{}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("PATCH -> %d", rec.Code)
	}
}

func TestResponsesAreJSONWithEmptyValuesAnObjectNotNull(t *testing.T) {
	w := newEdgeWorld(t, ":memory:")
	rec := getJSON(t, w.h, "/projects/p1/agent-system")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw["values"]) != "{}" {
		t.Errorf("values = %s, want {}", raw["values"])
	}
}
