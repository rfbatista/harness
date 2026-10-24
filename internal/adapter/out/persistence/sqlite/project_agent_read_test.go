package sqlite

import (
	"context"
	"testing"
)

func TestProjectRepo_AgentsReadAllReposIsOnUnlessTurnedOff(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewProjectRepository(db)
	ctx := context.Background()

	p, err := r.Create("harness", "/src/harness")
	if err != nil {
		t.Fatal(err)
	}
	if !p.AgentsReadAllRepos || !r.Get(ctx, p.ID).AgentsReadAllRepos {
		t.Fatal("a new project must read ON")
	}

	// A row stored before the column existed has no value for it.
	if err := db.Exec("INSERT INTO projects (id, name, root_dir) VALUES ('old', 'old', '/src/old')").Error; err != nil {
		t.Fatal(err)
	}
	if !r.Get(ctx, "old").AgentsReadAllRepos {
		t.Fatal("an older project row without the field must read ON")
	}

	off, err := r.SetAgentsReadAllRepos(p.ID, false)
	if err != nil || off.AgentsReadAllRepos {
		t.Fatalf("turned off = %+v, %v", off, err)
	}
	if r.Get(ctx, p.ID).AgentsReadAllRepos {
		t.Fatal("OFF was not stored")
	}
	if !r.Get(ctx, "old").AgentsReadAllRepos {
		t.Fatal("turning one project off changed another")
	}
	on, err := r.SetAgentsReadAllRepos(p.ID, true)
	if err != nil || !on.AgentsReadAllRepos || !r.Get(ctx, p.ID).AgentsReadAllRepos {
		t.Fatalf("turned back on = %+v, %v", on, err)
	}
	if _, err := r.SetAgentsReadAllRepos("ghost", false); err == nil {
		t.Fatal("unknown project accepted")
	}
}
