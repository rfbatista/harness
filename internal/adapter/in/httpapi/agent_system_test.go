package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
)

type agentSystemProjects map[string]*domain.Project

func (f agentSystemProjects) Get(_ context.Context, id string) *domain.Project { return f[id] }

type agentSystemRepositories map[string]*domain.Repository

func (f agentSystemRepositories) Get(_ context.Context, id string) *domain.Repository { return f[id] }

func agentSystemHandler(t *testing.T) *Handler {
	t.Helper()
	svc := agentsystem.New(agentsystem.Deps{
		Projects:     agentSystemProjects{"p1": {ID: "p1"}},
		Repositories: agentSystemRepositories{"r1": {ID: "r1", ProjectID: "p1"}},
		ReviewModel:  func() string { return "opus" },
	})
	return NewHandler(Services{AgentSystem: svc})
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
