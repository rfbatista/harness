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
