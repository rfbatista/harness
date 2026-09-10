package tooling

import (
	"context"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

func TestTicketTools_CreateAndList(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)

	tools := TicketTools(svc)
	byName := map[string]func(context.Context, map[string]any) (any, error){}
	for _, tl := range tools {
		byName[tl.Name] = tl.Handler
	}
	if byName["create_ticket"] == nil || byName["list_tickets"] == nil {
		t.Fatalf("expected create_ticket and list_tickets tools, got %v", byName)
	}

	if _, err := byName["create_ticket"](context.Background(), map[string]any{
		"project_id": p.ID, "title": "T1",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := byName["list_tickets"](context.Background(), map[string]any{"project_id": p.ID})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	lst, ok := m["tickets"].([]*domain.Ticket)
	if !ok || len(lst) != 1 {
		t.Fatalf("expected 1 ticket, got %#v", m["tickets"])
	}
}
