package httpclient_test

import (
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agents"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// These run the same contract suites the application services run, through
// the adapters, the real router and the real services: if a suite passes
// there and fails here, the wire lost something.

// serve serves s over the real router and returns a client of it.
func serve(t *testing.T, s httpapi.Services) *httpclient.Client {
	t.Helper()
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(s)))
	t.Cleanup(srv.Close)
	return httpclient.New(srv.URL)
}

func TestProjectsConformance(t *testing.T) {
	portstest.ProjectsConformance(t, func(t *testing.T) ports.Projects {
		db := openDB(t)
		svc := projects.NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
		return httpclient.NewProjects(serve(t, httpapi.Services{Projects: svc}))
	})
}

func TestTicketBoardConformance(t *testing.T) {
	portstest.TicketBoardConformance(t, func(t *testing.T) (ports.TicketBoard, string) {
		db := openDB(t)
		projectRepo := sqlite.NewProjectRepository(db)
		p, err := projectRepo.Create("proj", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projectRepo)
		return httpclient.NewPlanning(serve(t, httpapi.Services{Planning: svc})), p.ID
	})
}

func TestAgentCatalogConformance(t *testing.T) {
	portstest.AgentCatalogConformance(t, func(t *testing.T) ports.AgentCatalog {
		db := openDB(t)
		svc := agents.NewService(sqlite.NewAgentRepository(db), sqlite.NewPromptRepository(db), nil, nil)
		return httpclient.NewAgents(serve(t, httpapi.Services{Agents: svc}))
	})
}

func TestSessionReaderConformance(t *testing.T) {
	portstest.SessionReaderConformance(t, func(t *testing.T) (ports.SessionReader, *domain.Session) {
		db := openDB(t)
		repo := sqlite.NewSessionRepository(db)
		rec, err := repo.Create(&domain.Session{
			ID: "s1", ProjectID: "p1", TicketID: "tk1", Status: domain.SessionRunning, Interactive: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Reading sessions needs nothing but the store; the runtime is never
		// started.
		exe, _ := os.Executable()
		mgr := claude.New(llmkit.Options{Bin: exe, ApprovalTimeout: time.Hour})
		svc := orchestration.NewService(mgr, mgr.Approvals(), orchestration.NewHub(16), repo, orchestration.Catalog{}, nil, nil)
		return httpclient.NewSessions(serve(t, httpapi.Services{Sessions: svc})), rec
	})
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return db
}
