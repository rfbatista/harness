package architecture

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeProjects struct{}

func (fakeProjects) GetProject(_ context.Context, id string) (*domain.Project, error) {
	return &domain.Project{ID: id, Name: "Harness"}, nil
}

func (fakeProjects) ListProjects(context.Context) ([]*domain.Project, error) { return nil, nil }

type fakeRepositories struct{ rootDir string }

func (f fakeRepositories) ListRepositories(_ context.Context, projectID string) ([]*domain.Repository, error) {
	return []*domain.Repository{{ID: "r1", ProjectID: projectID, Name: "harness", RootDir: f.rootDir}}, nil
}

type fakeAssistant struct {
	a ports.ArchitectureAssistantAvailability
}

func (f fakeAssistant) Availability(_ context.Context, repositoryID string) ports.ArchitectureAssistantAvailability {
	if repositoryID != "r1" {
		panic("availability asked for " + repositoryID)
	}
	return f.a
}
func (f fakeAssistant) Readiness(context.Context) ports.ArchitectureAssistantAvailability { return f.a }

// render serves the page and answers its HTML.
func render(t *testing.T, h Handler) string {
	t.Helper()
	h.Projects = fakeProjects{}
	if h.Repositories == nil {
		h.Repositories = fakeRepositories{rootDir: "/work/harness"}
	}
	h.Layout = func(context.Context, string, string, string) (shell.Frame, error) { return shell.Frame{}, nil }
	var body bytes.Buffer
	h.Render = func(_ http.ResponseWriter, r *http.Request, _ int, page templ.Component) error {
		return page.Render(r.Context(), &body)
	}
	req := httptest.NewRequest(http.MethodGet, "/projects/p1/repositories/r1/architecture", nil)
	req.SetPathValue("project", "p1")
	req.SetPathValue("repository", "r1")
	if err := h.Page("")(httptest.NewRecorder(), req); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestTheSeedSaysTheAssistantIsAvailable(t *testing.T) {
	html := render(t, Handler{Assistant: fakeAssistant{a: ports.ArchitectureAssistantAvailability{Available: true}}})
	for _, want := range []string{`"assistant_available":true`, `"assistant_reason":""`, `"assistant_fix_url":""`} {
		if !strings.Contains(html, want) {
			t.Errorf("the seed lacks %s:\n%s", want, html)
		}
	}
}

func TestTheSeedCarriesTheReasonAndFixURLWhenTheAssistantCannotRun(t *testing.T) {
	html := render(t, Handler{Assistant: fakeAssistant{a: ports.ArchitectureAssistantAvailability{Reason: "no agent credential", FixURL: "/agent-isolation#credential"}}})
	for _, want := range []string{`"assistant_available":false`, `"assistant_reason":"no agent credential"`, `"assistant_fix_url":"/agent-isolation#credential"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the seed lacks %s:\n%s", want, html)
		}
	}
}

func TestWithoutAnAssistantTheSeedSaysItIsUnavailableAndTheRestIsAsBefore(t *testing.T) {
	html := render(t, Handler{})
	for _, want := range []string{`"assistant_available":false`, `"repository_id":"r1"`, `"root_dir":"/work/harness"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the seed lacks %s", want)
		}
	}
	if !strings.Contains(html, `"assistant_reason":"`) || strings.Contains(html, `"assistant_reason":""`) {
		t.Errorf("an unconfigured assistant gives no reason:\n%s", html)
	}
}
