package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const gb = int64(1) << 30

// oneTask is the only task the memory budget tests know.
type oneTask struct{ projectID, id string }

func (o oneTask) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	if id != o.id {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "task not found"}
	}
	return &domain.Ticket{ID: id, ProjectID: o.projectID}, nil
}

func (oneTask) ListTickets(context.Context, string, ports.TicketFilter) ([]*domain.Ticket, error) {
	return nil, nil
}

// budgetWorld is a handler over a real catalog with one project, one
// repository and one task.
type budgetWorld struct {
	h          *Handler
	repository string
	task       string
}

func newBudgetWorld(t *testing.T) *budgetWorld {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(catalog.SQLiteDeps(db))
	ctx := context.Background()
	p, err := cat.Projects.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(ctx, p.ID, "api", "", "https://example.com/api.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat.Capabilities.UseTasks(oneTask{projectID: p.ID, id: "t1"})
	svc := servicesOf(cat)
	svc.MemoryBudgets = cat.Capabilities
	return &budgetWorld{h: NewHandler(svc), repository: r.ID, task: "t1"}
}

func decodeOverride(t *testing.T, body []byte) domain.MemoryBudgetOverride {
	t.Helper()
	var got domain.MemoryBudgetOverride
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return got
}

// The global orçamento is a setting like the others: read back, validated.
func TestTheGlobalOrcamentoDeMemoriaIsEditedThroughTheSettings(t *testing.T) {
	w := newBudgetWorld(t)

	if rec := postJSON(t, w.h, "/update_settings", map[string]any{"settings": map[string]string{domain.SettingMemoryBudget: "6442450944"}}); rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body)
	}
	var got struct{ Settings map[string]string }
	rec := getJSON(t, w.h, "/get_settings")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Settings[domain.SettingMemoryBudget] != "6442450944" {
		t.Errorf("settings = %v (%v), want the saved orçamento", got.Settings, err)
	}
	for _, bad := range []string{"0", "-5", "lots", "4GB"} {
		if rec := postJSON(t, w.h, "/update_settings", map[string]any{"settings": map[string]string{domain.SettingMemoryBudget: bad}}); rec.Code == http.StatusOK {
			t.Errorf("orçamento %q was accepted", bad)
		}
	}
}

// A repository or task sets its own orçamento, reads it back, and clears it to inherit.
func TestARepositoryOrTaskOverridesTheOrcamentoAndClearsItToInherit(t *testing.T) {
	w := newBudgetWorld(t)

	for _, owner := range []struct{ level, id string }{{"repository", w.repository}, {"task", w.task}} {
		path := "/get_memory_budget_override?level=" + owner.level + "&owner_id=" + owner.id
		got := decodeOverride(t, getJSON(t, w.h, path).Body.Bytes())
		if got.Bytes != 0 || got.EffectiveBytes != 4*gb {
			t.Errorf("%s inherits: %+v, want bytes 0 and effective 4 GB", owner.level, got)
		}
		rec := postJSON(t, w.h, "/set_memory_budget_override", map[string]any{"level": owner.level, "owner_id": owner.id, "bytes": 8 * gb})
		if rec.Code != http.StatusOK {
			t.Fatalf("set %s = %d: %s", owner.level, rec.Code, rec.Body)
		}
		if got := decodeOverride(t, rec.Body.Bytes()); got.Bytes != 8*gb || got.EffectiveBytes != 8*gb {
			t.Errorf("%s after set: %+v, want 8 GB both", owner.level, got)
		}
		if got := decodeOverride(t, getJSON(t, w.h, path).Body.Bytes()); got.Bytes != 8*gb {
			t.Errorf("%s reads back %+v, want 8 GB", owner.level, got)
		}
		rec = postJSON(t, w.h, "/set_memory_budget_override", map[string]any{"level": owner.level, "owner_id": owner.id, "bytes": 0})
		if got := decodeOverride(t, rec.Body.Bytes()); got.Bytes != 0 || got.EffectiveBytes != 4*gb {
			t.Errorf("%s after clearing: %+v, want inherit", owner.level, got)
		}
	}
}

func TestAnOrcamentoOverrideIsRefusedForAProjectABadValueOrAnUnknownOwner(t *testing.T) {
	w := newBudgetWorld(t)
	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"project level", map[string]any{"level": "project", "owner_id": "p1", "bytes": gb}, http.StatusBadRequest},
		{"negative", map[string]any{"level": "repository", "owner_id": w.repository, "bytes": -1}, http.StatusBadRequest},
		{"unknown task", map[string]any{"level": "task", "owner_id": "nope", "bytes": gb}, http.StatusNotFound},
	} {
		if rec := postJSON(t, w.h, "/set_memory_budget_override", c.body); rec.Code != c.want {
			t.Errorf("%s = %d (%s), want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
}
