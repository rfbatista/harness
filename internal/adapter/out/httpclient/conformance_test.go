package httpclient_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/blobstore"
	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agents"
	"operators-mcp/internal/application/artifacts/artifactstest"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/application/workspaces/workspacestest"
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

func TestSessionFeedConformance(t *testing.T) {
	portstest.SessionFeedConformance(t, func(t *testing.T) (ports.SessionFeed, string, func() string) {
		repo := sqlite.NewSessionRepository(openDB(t))
		exe, _ := os.Executable()
		mgr := claude.New(llmkit.Options{Bin: exe, ApprovalTimeout: time.Hour})
		svc := orchestration.NewService(mgr, mgr.Approvals(), orchestration.NewHub(16), repo, orchestration.Catalog{}, nil, nil)
		n := 0
		// A RunnerTUI session the client reports ending: the end is a change.
		change := func() string {
			n++
			id := fmt.Sprintf("s%d", n)
			if _, err := repo.Create(&domain.Session{ID: id, ProjectID: "p1", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EndInteractive(context.Background(), id, 0, false); err != nil {
				t.Fatal(err)
			}
			return id
		}
		return httpclient.NewEvents(serve(t, httpapi.Services{Sessions: svc})), "p1", change
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

func TestArtifactAttachmentsConformance(t *testing.T) {
	portstest.ArtifactAttachmentsConformance(t, func(t *testing.T) (ports.ArtifactAttachments, portstest.AttachmentFixture) {
		svc, f := artifactstest.New(t)
		return httpclient.NewArtifacts(serve(t, httpapi.Services{Artifacts: svc})), f
	})
}

// resourcesServer serves a planning service with resources and comments
// over the real router, with a task to put them on.
func resourcesServer(t *testing.T) (*httpclient.Client, string) {
	t.Helper()
	db := openDB(t)
	projectRepo := sqlite.NewProjectRepository(db)
	p, err := projectRepo.Create("proj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projectRepo)
	svc.Resources, svc.Comments = sqlite.NewTicketResourceRepository(db), sqlite.NewTicketCommentRepository(db)
	svc.Blobs, svc.ContentURL = blobstore.New(t.TempDir()), httpapi.TicketResourceContentPath
	tk, err := svc.CreateTicket(context.Background(), p.ID, "Task", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, httpapi.Services{Planning: svc, TaskResources: svc, TaskComments: svc}), tk.ID
}

// sessionCommentsServer is resourcesServer that also answers how to write a
// comment as a session, in process: the API has no route for it.
func sessionCommentsServer(t *testing.T) (*httpclient.Client, string, portstest.SessionCommentSeed) {
	t.Helper()
	db := openDB(t)
	projectRepo := sqlite.NewProjectRepository(db)
	p, err := projectRepo.Create("proj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projectRepo)
	svc.Comments = sqlite.NewTicketCommentRepository(db)
	tk, err := svc.CreateTicket(context.Background(), p.ID, "Task", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(t *testing.T, tid string, w domain.CommentWriter, body string) *domain.TicketComment {
		c, err := svc.AddSessionComment(context.Background(), tid, w, body)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	return serve(t, httpapi.Services{Planning: svc, TaskComments: svc}), tk.ID, seed
}

func TestTaskResourcesConformance(t *testing.T) {
	portstest.TaskResourcesConformance(t, func(t *testing.T) (ports.TaskResources, string) {
		c, tid := resourcesServer(t)
		return httpclient.NewTaskResources(c), tid
	})
}

func TestTaskCommentsConformance(t *testing.T) {
	portstest.TaskCommentsConformance(t, func(t *testing.T) (ports.TaskComments, string) {
		c, tid := resourcesServer(t)
		return httpclient.NewTaskComments(c), tid
	})
}

func TestNotificationsConformance(t *testing.T) {
	portstest.NotificationsConformance(t, func(t *testing.T) (ports.Notifications, portstest.NotificationsFixture) {
		svc, f := notificationstest.New(t)
		return httpclient.NewNotifications(serve(t, httpapi.Services{Notifications: svc})), f
	})
}

func TestCodeConformance(t *testing.T) {
	portstest.CodeConformance(t, func(t *testing.T, f *portstest.CodeFixture) (ports.CodeBrowser, portstest.CodeTarget) {
		svc, at := workspacestest.NewCode(t, f)
		return httpclient.NewCode(serve(t, httpapi.Services{Code: svc})), at
	})
}

func TestSessionCommentsConformance(t *testing.T) {
	portstest.SessionCommentsConformance(t, func(t *testing.T) (ports.TaskComments, string, portstest.SessionCommentSeed) {
		c, tid, seed := sessionCommentsServer(t)
		return httpclient.NewTaskComments(c), tid, seed
	})
}

func TestGitCredentialsConformance(t *testing.T) {
	portstest.GitCredentialsConformance(t, func(t *testing.T) (ports.GitCredentials, portstest.GitCredentialsFixture) {
		env := gitcredentialstest.NewSQLite(t)
		catalog := projects.NewService(env.Projects, env.Repos, nil)
		c := serve(t, httpapi.Services{Projects: catalog, ProjectFeed: catalog, GitCredentials: env.Service})
		return httpclient.NewGitCredentials(c), env.Fixture()
	})
}
