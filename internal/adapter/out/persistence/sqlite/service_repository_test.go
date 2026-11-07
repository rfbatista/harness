package sqlite

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestServiceRepository_CountsOnlyStartingAndRunningPerProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewServiceRepository(db)
	code := 3
	for i, c := range []struct {
		project string
		status  domain.ServiceStatus
	}{
		{"p1", domain.ServiceStopped}, {"p1", domain.ServiceStarting}, {"p1", domain.ServiceRunning},
		{"p1", domain.ServiceExited}, {"p1", domain.ServiceFailed}, {"p2", domain.ServiceRunning},
	} {
		s := &domain.Service{ID: string(rune('a' + i)), ProjectID: c.project, Name: string(rune('a' + i)), Kind: domain.ServiceKindCommand,
			Command: &domain.ServiceCommand{Line: "x"}, Status: c.status}
		if c.status == domain.ServiceExited {
			s.Run = &domain.ServiceRun{ID: "r", ExitCode: &code}
		}
		if err := r.SaveService(s); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.RunningServices("p1"); err != nil || n != 2 {
		t.Fatalf("p1 running = %d, %v", n, err)
	}
	got, err := r.GetService("d")
	if err != nil || got.Run == nil || *got.Run.ExitCode != 3 || got.Command.Line != "x" || got.Image != nil {
		t.Fatalf("the last run and definition round-trip: %+v %v", got, err)
	}
	if err := r.SaveService(&domain.Service{ID: "z", ProjectID: "p1", Name: "a", Kind: domain.ServiceKindCommand}); err == nil {
		t.Fatal("two services of a project cannot share a name")
	}
}

func TestServiceRepository_ActiveServicesAreTheStartingAndRunningOnesOfEveryProject(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewServiceRepository(db)
	for _, c := range []struct {
		id, project, name string
		status            domain.ServiceStatus
	}{
		{"a", "p2", "web", domain.ServiceRunning}, {"b", "p1", "zed", domain.ServiceStarting}, {"c", "p1", "api", domain.ServiceRunning},
		{"d", "p1", "old", domain.ServiceStopped}, {"e", "p2", "bad", domain.ServiceFailed}, {"f", "p1", "done", domain.ServiceExited},
	} {
		s := &domain.Service{ID: c.id, ProjectID: c.project, Name: c.name, Kind: domain.ServiceKindCommand, Command: &domain.ServiceCommand{Line: "x"}, Status: c.status}
		if err := r.SaveService(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ActiveServices()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if want := []string{"c", "b", "a"}; len(ids) != 3 || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("active = %v, want %v (by project then name)", ids, want)
	}
}
