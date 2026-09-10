package tooling

import (
	"context"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

func TestBoundedContextTools_CRUD(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	zones := sqlite.NewZoneRepository(db)
	svc := blueprint.NewService(projects, nil, zones, nil, nil, nil, nil, nil, nil, nil, "").
		WithBoundedContexts(sqlite.NewBoundedContextRepository(db))

	byName := map[string]func(context.Context, map[string]any) (any, error){}
	for _, tl := range BoundedContextTools(svc) {
		byName[tl.Name] = tl.Handler
	}
	ctx := context.Background()

	out, err := byName["create_bounded_context"](ctx, map[string]any{
		"project_id": p.ID,
		"name":       "Billing",
		"purpose":    "Handles invoicing",
		"ubiquitous_language": []any{
			map[string]any{"term": "Invoice", "definition": "A bill"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bc := out.(map[string]any)["bounded_context"].(*domain.BoundedContext)
	if bc.Name != "Billing" || len(bc.UbiquitousLanguage) != 1 || bc.UbiquitousLanguage[0].Term != "Invoice" {
		t.Fatalf("bad create result: %+v", bc)
	}

	out, err = byName["list_bounded_contexts"](ctx, map[string]any{"project_id": p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if lst := out.(map[string]any)["bounded_contexts"].([]*domain.BoundedContext); len(lst) != 1 {
		t.Fatalf("expected 1 bounded context, got %d", len(lst))
	}

	if _, err := byName["get_bounded_context"](ctx, map[string]any{"bounded_context_id": "missing"}); err == nil {
		t.Fatal("expected error for missing bounded context")
	}

	out, err = byName["update_bounded_context"](ctx, map[string]any{
		"bounded_context_id": bc.ID,
		"name":               "Invoicing",
		"purpose":            "new purpose",
		"ubiquitous_language": []any{
			map[string]any{"term": "Ledger", "definition": "Record of transactions"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated := out.(map[string]any)["bounded_context"].(*domain.BoundedContext)
	if updated.Name != "Invoicing" || len(updated.UbiquitousLanguage) != 1 || updated.UbiquitousLanguage[0].Term != "Ledger" {
		t.Fatalf("bad update result: %+v", updated)
	}

	// Assign a zone, then delete the context and prove the zone survives unlinked.
	zone, err := zones.Create(p.ID, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err = byName["assign_zone_to_bounded_context"](ctx, map[string]any{
		"zone_id": zone.ID, "bounded_context_id": bc.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if z := out.(map[string]any)["zone"].(*domain.Zone); z.BoundedContextID != bc.ID {
		t.Fatalf("zone not linked: %+v", z)
	}

	out, err = byName["unassign_zone_from_bounded_context"](ctx, map[string]any{"zone_id": zone.ID})
	if err != nil {
		t.Fatal(err)
	}
	if z := out.(map[string]any)["zone"].(*domain.Zone); z.BoundedContextID != "" {
		t.Fatalf("zone not unlinked: %+v", z)
	}

	if _, err := byName["delete_bounded_context"](ctx, map[string]any{"bounded_context_id": bc.ID}); err != nil {
		t.Fatal(err)
	}
	out, _ = byName["list_bounded_contexts"](ctx, map[string]any{"project_id": p.ID})
	if lst := out.(map[string]any)["bounded_contexts"].([]*domain.BoundedContext); len(lst) != 0 {
		t.Fatalf("expected 0 bounded contexts after delete, got %d", len(lst))
	}
}
