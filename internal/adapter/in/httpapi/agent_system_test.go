package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"bytes"
	"net/http/httptest"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type agentSystemProjects map[string]*domain.Project

func (f agentSystemProjects) Get(_ context.Context, id string) *domain.Project { return f[id] }

type agentSystemRepositories map[string]*domain.Repository

func (f agentSystemRepositories) Get(_ context.Context, id string) *domain.Repository { return f[id] }

type feedRecorder struct{ changes []ports.ProjectChange }

func (f *feedRecorder) AnnounceChange(c ports.ProjectChange) { f.changes = append(f.changes, c) }

func agentSystemHandler(t *testing.T) *Handler {
	h, _ := agentSystemHandlerWithFeed(t)
	return h
}

func agentSystemHandlerWithFeed(t *testing.T) (*Handler, *feedRecorder) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	feed := &feedRecorder{}
	svc := agentsystem.New(agentsystem.Deps{
		Store:        sqlite.NewAgentSystemRepository(db),
		Feed:         feed,
		Projects:     agentSystemProjects{"p1": {ID: "p1"}},
		Repositories: agentSystemRepositories{"r1": {ID: "r1", ProjectID: "p1"}},
		ReviewModel:  func() string { return "opus" },
	})
	return NewHandler(Services{AgentSystem: svc}), feed
}

func TestGetProjectAgentSystemShowsHarnessDefaultsWithTheirSource(t *testing.T) {
	h := agentSystemHandler(t)

	rec := getJSON(t, h, "/projects/p1/agent-system")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Effective struct {
			Guard struct {
				RetryLimit struct {
					Value  int    `json:"value"`
					Source string `json:"source"`
				} `json:"retry_limit"`
			} `json:"guard"`
		} `json:"effective"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body.Effective.Guard.RetryLimit; got.Value != 3 || got.Source != "harness" {
		t.Fatalf("retry_limit = %+v, want 3 from harness", got)
	}
}

func TestGetProjectAgentSystemOfAnUnknownProjectIs404(t *testing.T) {
	h := agentSystemHandler(t)

	if rec := getJSON(t, h, "/projects/nope/agent-system"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func patchJSON(t *testing.T, h *Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api"+path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	return rec
}

type agentSystemErrors struct {
	Errors []struct {
		Field   string `json:"field"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func TestPatchProjectAgentSystemSetsAndResetsFields(t *testing.T) {
	h := agentSystemHandler(t)

	rec := patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": 7}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var view struct {
		Effective struct {
			Guard struct {
				RetryLimit struct {
					Value  int    `json:"value"`
					Source string `json:"source"`
				} `json:"retry_limit"`
			} `json:"guard"`
		} `json:"effective"`
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if got := view.Effective.Guard.RetryLimit; got.Value != 7 || got.Source != "project" || view.Values["guard.retry_limit"] != float64(7) {
		t.Fatalf("view = %+v / %v, want 7 from project", got, view.Values)
	}

	rec = patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": null}`)
	view.Values = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || view.Effective.Guard.RetryLimit.Source != "harness" || len(view.Values) != 0 {
		t.Fatalf("after reset: %s", rec.Body.String())
	}
}

func TestPatchProjectAgentSystemRefusesWith422AndTheFieldNamed(t *testing.T) {
	h := agentSystemHandler(t)

	for body, want := range map[string][2]string{
		`{"guard.retry_limit": 0}`:  {"guard.retry_limit", "OUT_OF_RANGE"},
		`{"guard.retry_limit": 11}`: {"guard.retry_limit", "OUT_OF_RANGE"},
		`{"guard.nope": true}`:      {"guard.nope", "UNKNOWN_FIELD"},
	} {
		rec := patchJSON(t, h, "/projects/p1/agent-system", body)
		var e agentSystemErrors
		if rec.Code != http.StatusUnprocessableEntity || json.Unmarshal(rec.Body.Bytes(), &e) != nil || len(e.Errors) != 1 || e.Errors[0].Field != want[0] || e.Errors[0].Code != want[1] || e.Errors[0].Message == "" {
			t.Errorf("%s -> %d %s, want 422 %v", body, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestPatchProjectAgentSystemIsPushedOnTheProjectFeed(t *testing.T) {
	h, feed := agentSystemHandlerWithFeed(t)

	patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": 7}`)

	if len(feed.changes) != 1 || feed.changes[0].AgentSystemSettings == nil || feed.changes[0].ProjectID() != "p1" {
		t.Fatalf("feed = %+v", feed.changes)
	}
	raw, _ := json.Marshal(feed.changes[0])
	if !bytes.Contains(raw, []byte(`"agent_system_settings"`)) {
		t.Fatalf("wire = %s, want the agent_system_settings key", raw)
	}
}

func TestPatchOfAnUnknownProjectIs404(t *testing.T) {
	if rec := patchJSON(t, agentSystemHandler(t), "/projects/nope/agent-system", `{"guard.retry_limit": 7}`); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRepositoryAgentSystemShowsSourcesOverridesAndMissingGates(t *testing.T) {
	h := agentSystemHandler(t)
	patchJSON(t, h, "/projects/p1/agent-system", `{"guard.retry_limit": 5}`)
	rec := patchJSON(t, h, "/projects/p1/repositories/r1/agent-system", `{"learn.context_map.enabled": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/projects/p1/repositories/r1/agent-system")
	var view struct {
		Effective struct {
			Learn struct {
				ContextMap struct {
					Enabled struct {
						Value  bool   `json:"value"`
						Source string `json:"source"`
					} `json:"enabled"`
				} `json:"context_map"`
			} `json:"learn"`
			Guard struct {
				RetryLimit struct {
					Value  int    `json:"value"`
					Source string `json:"source"`
				} `json:"retry_limit"`
			} `json:"guard"`
			Missing []string `json:"missing_required_gates"`
		} `json:"effective"`
		Values map[string]any `json:"values"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &view) != nil {
		t.Fatalf("get = %d %s", rec.Code, rec.Body.String())
	}
	if e := view.Effective; e.Learn.ContextMap.Enabled.Source != "repository" || e.Learn.ContextMap.Enabled.Value || e.Guard.RetryLimit.Source != "project" || len(e.Missing) != 5 {
		t.Fatalf("effective = %+v", e)
	}
	if len(view.Values) != 1 || view.Values["learn.context_map.enabled"] != false {
		t.Fatalf("overrides = %v", view.Values)
	}

	view.Values = nil
	rec = patchJSON(t, h, "/projects/p1/repositories/r1/agent-system", `{"learn.context_map.enabled": null}`)
	if json.Unmarshal(rec.Body.Bytes(), &view) != nil || view.Effective.Learn.ContextMap.Enabled.Source != "harness" || len(view.Values) != 0 {
		t.Fatalf("after reset: %s", rec.Body.String())
	}
}

func TestRepositoryAgentSystemRefusesLikeTheProjectAndKnowsItsProject(t *testing.T) {
	h := agentSystemHandler(t)

	rec := patchJSON(t, h, "/projects/p1/repositories/r1/agent-system", `{"guard.retry_limit": 99}`)
	var e agentSystemErrors
	if rec.Code != http.StatusUnprocessableEntity || json.Unmarshal(rec.Body.Bytes(), &e) != nil || len(e.Errors) != 1 || e.Errors[0].Code != "OUT_OF_RANGE" {
		t.Errorf("422 shape: %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/projects/p1/repositories/nope/agent-system", "/projects/nope/repositories/r1/agent-system"} {
		if rec := getJSON(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}
