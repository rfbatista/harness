package tooling

import (
	"bytes"
	"context"
	"encoding/json"
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

// update_ticket fronts the same partial use case the HTTP route does: a field
// the agent leaves out keeps its value.
func TestTicketTools_UpdateIsPartial(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	byName := map[string]domain.Tool{}
	for _, tl := range TicketTools(svc) {
		byName[tl.Name] = tl
	}
	ctx := context.Background()

	out, err := byName["create_ticket"].Handler(ctx, map[string]any{"project_id": p.ID, "title": "Ship it", "description": "with care", "status": "todo"})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["ticket"].(*domain.Ticket).ID

	out, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "status": "review"})
	if err != nil {
		t.Fatal(err)
	}
	if tk := out.(map[string]any)["ticket"].(*domain.Ticket); tk.Status != domain.TicketStatusReview || tk.Title != "Ship it" || tk.Description != "with care" {
		t.Fatalf("status-only update touched the text: %+v", tk)
	}

	out, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "title": "Ship it now", "description": ""})
	if err != nil {
		t.Fatal(err)
	}
	if tk := out.(map[string]any)["ticket"].(*domain.Ticket); tk.Status != domain.TicketStatusReview || tk.Title != "Ship it now" || tk.Description != "" {
		t.Fatalf("text update moved the card or kept the description: %+v", tk)
	}

	// Every field, as callers send today.
	out, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "title": "T", "description": "d", "status": "done"})
	if err != nil {
		t.Fatal(err)
	}
	if tk := out.(map[string]any)["ticket"].(*domain.Ticket); tk.Status != domain.TicketStatusDone || tk.Title != "T" || tk.Description != "d" {
		t.Fatalf("full update = %+v", tk)
	}

	// null is "untouched", as over HTTP; blank is rejected.
	out, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "title": nil, "status": "review"})
	if err != nil {
		t.Fatal(err)
	}
	if tk := out.(map[string]any)["ticket"].(*domain.Ticket); tk.Title != "T" || tk.Status != domain.TicketStatusReview {
		t.Fatalf("null title changed the ticket: %+v", tk)
	}
	_, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "title": ""})
	wantCode(t, err, "INVALID_INPUT")
	_, err = byName["update_ticket"].Handler(ctx, map[string]any{"ticket_id": id, "status": "shipped"})
	wantCode(t, err, "INVALID_INPUT")

	// The schema no longer demands a title.
	if raw, _ := json.Marshal(byName["update_ticket"].InputSchema); !bytes.Contains(raw, []byte(`"required":["ticket_id"]`)) {
		t.Fatalf("schema still requires more than ticket_id: %s", raw)
	}
}
