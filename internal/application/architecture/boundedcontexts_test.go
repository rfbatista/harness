package architecture

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// testService is the architecture context over an in-memory database, with its
// repositories exposed so a test can arrange state directly.
type testService struct {
	*Service
	Projects ports.ProjectRepository
	Zones    ports.ZoneRepository
}

// projectReader reads projects straight from the repository; the projects
// context is not needed to test this one.
type projectReader struct{ ports.ProjectRepository }

func (r projectReader) GetProject(_ context.Context, id string) (*domain.Project, error) {
	if p := r.Get(id); p != nil {
		return p, nil
	}
	return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
}

func (r projectReader) ListProjects(context.Context) ([]*domain.Project, error) { return r.List(), nil }

func newBoundedContextTestService(t *testing.T) (testService, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	zones := sqlite.NewZoneRepository(db)
	svc := NewService(Deps{
		Zones:           zones,
		BoundedContexts: sqlite.NewBoundedContextRepository(db),
		Projects:        projectReader{projects},
	})
	return testService{Service: svc, Projects: projects, Zones: zones}, p.ID
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestCreateBoundedContext_RequiresProject(t *testing.T) {
	svc, _ := newBoundedContextTestService(t)
	_, err := svc.CreateBoundedContext("missing", "Billing", "", nil)
	wantCode(t, err, "PROJECT_NOT_FOUND")
}

func TestCreateAndListBoundedContexts(t *testing.T) {
	svc, pid := newBoundedContextTestService(t)
	created, err := svc.CreateBoundedContext(pid, "Billing", "Handles invoicing", []domain.LanguageTerm{
		{Term: "Invoice", Definition: "A bill"},
	})
	if err != nil {
		t.Fatal(err)
	}
	list := svc.ListBoundedContexts(pid)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("want 1 context %q, got %d", created.ID, len(list))
	}
	if got := svc.GetBoundedContext(created.ID); got == nil || len(got.UbiquitousLanguage) != 1 {
		t.Fatalf("GetBoundedContext failed: %+v", got)
	}
}

func TestUpdateBoundedContext_ReplacesLanguage(t *testing.T) {
	svc, pid := newBoundedContextTestService(t)
	created, err := svc.CreateBoundedContext(pid, "Billing", "", []domain.LanguageTerm{
		{Term: "Invoice", Definition: "A bill"},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateBoundedContext(created.ID, "Billing", "new purpose", []domain.LanguageTerm{
		{Term: "Ledger", Definition: "Record of transactions"},
		{Term: "Payment", Definition: "Money received"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Purpose != "new purpose" || len(updated.UbiquitousLanguage) != 2 || updated.UbiquitousLanguage[0].Term != "Ledger" {
		t.Fatalf("language not replaced: %+v", updated)
	}
}

func TestDeleteBoundedContext_ClearsZoneLinks(t *testing.T) {
	svc, pid := newBoundedContextTestService(t)
	bc, err := svc.CreateBoundedContext(pid, "Billing", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.Zones.Create(pid, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssignZoneToBoundedContext(zone.ID, bc.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteBoundedContext(bc.ID); err != nil {
		t.Fatal(err)
	}
	// The zone must survive, unlinked.
	got := svc.Zones.Get(zone.ID)
	if got == nil {
		t.Fatal("zone should survive context deletion")
	}
	if got.BoundedContextID != "" {
		t.Fatalf("zone should be unlinked, got %q", got.BoundedContextID)
	}

	wantCode(t, svc.DeleteBoundedContext("missing"), "BOUNDED_CONTEXT_NOT_FOUND")
}

func TestAssignZoneToBoundedContext_CrossProjectRejected(t *testing.T) {
	svc, pid := newBoundedContextTestService(t)
	other, err := svc.Projects.Create("other", "/tmp/other")
	if err != nil {
		t.Fatal(err)
	}
	bc, err := svc.CreateBoundedContext(pid, "Billing", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.Zones.Create(other.ID, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AssignZoneToBoundedContext(zone.ID, bc.ID)
	wantCode(t, err, "CROSS_PROJECT_ACCESS")
}

func TestAssignZoneToBoundedContext_SetsAndClears(t *testing.T) {
	svc, pid := newBoundedContextTestService(t)
	bc, err := svc.CreateBoundedContext(pid, "Billing", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zone, err := svc.Zones.Create(pid, "api", "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	linked, err := svc.AssignZoneToBoundedContext(zone.ID, bc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.BoundedContextID != bc.ID {
		t.Fatalf("zone not linked: %+v", linked)
	}

	_, err = svc.AssignZoneToBoundedContext(zone.ID, "missing")
	wantCode(t, err, "BOUNDED_CONTEXT_NOT_FOUND")
	_, err = svc.AssignZoneToBoundedContext("missing", bc.ID)
	wantCode(t, err, "ZONE_NOT_FOUND")

	unlinked, err := svc.UnassignZoneFromBoundedContext(zone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unlinked.BoundedContextID != "" {
		t.Fatalf("zone not unlinked: %+v", unlinked)
	}
}
