// Package artifactstest builds an artifacts service with a project asset
// ready to attach, for the attachment contract suite on either side of the
// wire.
package artifactstest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// Tickets is ports.TicketReader over the ticket table the links join.
type Tickets struct{ Repo *sqlite.TicketRepository }

func (t Tickets) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	if tk := t.Repo.Get(id); tk != nil {
		return tk, nil
	}
	return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
}

func (t Tickets) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	return t.Repo.ListByProject(projectID), nil
}

// New returns an artifacts service over an in-memory database holding tasks
// tk1 (producer) and tk2 of project p1 and tk9 of p2, a project asset
// published by session s1 on tk1, and a task artifact of tk1.
func New(t *testing.T) (*artifacts.Service, portstest.AttachmentFixture) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range []sqlite.TicketModel{
		{ID: "tk1", ProjectID: "p1", Title: "Design"}, {ID: "tk2", ProjectID: "p1", Title: "Board"}, {ID: "tk9", ProjectID: "p2", Title: "Elsewhere"},
	} {
		if err := db.Create(&tk).Error; err != nil {
			t.Fatal(err)
		}
	}
	sessions := sqlite.NewSessionRepository(db)
	root := t.TempDir()
	if _, err := sessions.Create(&domain.Session{ID: "s1", ProjectID: "p1", TicketID: "tk1", Task: "design", WorkingDir: root, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
	svc := artifacts.NewService(sqlite.NewArtifactRepository(db), sessions, nil)
	svc.StoreDir = t.TempDir()
	svc.Tickets = Tickets{Repo: sqlite.NewTicketRepository(db)}

	ctx := context.Background()
	publish := func(rel string) *domain.Artifact {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("<p>"+rel+"</p>"), 0o644); err != nil {
			t.Fatal(err)
		}
		a, err := svc.Publish(ctx, ports.PublishArtifactRequest{SessionID: "s1", Path: rel, Title: rel})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	asset := publish("logo.html")
	if _, err := svc.SetArtifactScope(ctx, asset.ID, domain.ArtifactScopeProject); err != nil {
		t.Fatal(err)
	}
	draft := publish("draft.html")
	return svc, portstest.AttachmentFixture{AssetID: asset.ID, Producer: "tk1", Other: "tk2", Foreign: "tk9", TaskArtifactID: draft.ID}
}
