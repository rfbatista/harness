package planning_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/ports"
)

// A task carries the task board's fields from the start: unarchived, in its
// status since it was created, never moved, and with no merge state.
func TestATaskCarriesTheBoardFieldsWithTheirDefaults(t *testing.T) {
	svc, pid := newService(t)
	ctx := context.Background()
	tk, err := svc.CreateTicket(ctx, pid, "T", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"archived_at", "last_move", "merge_state"} {
		v, ok := m[key]
		if !ok {
			t.Errorf("%s is missing from %s", key, raw)
		} else if v != nil {
			t.Errorf("%s = %v, want null", key, v)
		}
	}
	if m["status_since"] != m["created_at"] {
		t.Errorf("status_since = %v, want created_at %v", m["status_since"], m["created_at"])
	}
	for _, key := range []string{"id", "project_id", "title", "status", "created_at", "updated_at", "architect_session_id", "pending_reviews"} {
		if _, ok := m[key]; !ok {
			t.Errorf("existing key %s is gone from %s", key, raw)
		}
	}
}

// A database written before tasks could be archived opens, gains the column,
// and lists its tasks unarchived.
func TestADatabaseFromBeforeArchivingListsItsTasksUnarchived(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.NewTicketRepository(db).Create(p.ID, "old", "", "done", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE tickets DROP COLUMN archived_at").Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}

	db, err = sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	projects = sqlite.NewProjectRepository(db)
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	tickets, err := svc.ListTickets(context.Background(), p.ID, ports.TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].ArchivedAt != nil {
		t.Fatalf("tickets = %+v, want the old task unarchived", tickets)
	}
}
