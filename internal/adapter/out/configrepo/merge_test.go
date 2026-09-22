package configrepo

import (
	"testing"

	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// fakeAgentRepo is a minimal in-memory stand-in for a database-backed
// ports.AgentRepository, just enough to exercise MergedAgentRepository.
type fakeAgentRepo struct {
	byID    map[string]*domain.Agent
	created bool
	updated bool
	deleted bool
}

func newFakeAgentRepo(agents ...*domain.Agent) *fakeAgentRepo {
	r := &fakeAgentRepo{byID: map[string]*domain.Agent{}}
	for _, a := range agents {
		r.byID[a.ID] = a
	}
	return r
}

func (r *fakeAgentRepo) Get(id string) *domain.Agent { return r.byID[id] }
func (r *fakeAgentRepo) List() []*domain.Agent {
	out := make([]*domain.Agent, 0, len(r.byID))
	for _, a := range r.byID {
		out = append(out, a)
	}
	return out
}
func (r *fakeAgentRepo) Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	r.created = true
	return &domain.Agent{ID: "db-new", Name: name}, nil
}
func (r *fakeAgentRepo) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	r.updated = true
	return r.byID[id], nil
}
func (r *fakeAgentRepo) Delete(id string) error {
	r.deleted = true
	delete(r.byID, id)
	return nil
}

func TestMergedAgentRepository_ListCombinesBothSources(t *testing.T) {
	db := newFakeAgentRepo(&domain.Agent{ID: "db-1", Name: "designer"})
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer"})
	merged := MergeAgents(db, cfg)

	names := map[string]bool{}
	for _, a := range merged.List() {
		names[a.Name] = true
	}
	if !names["designer"] || !names["go-developer"] {
		t.Fatalf("expected both sources represented, got %v", names)
	}
}

func TestMergedAgentRepository_ConfigWinsOnNameCollision(t *testing.T) {
	db := newFakeAgentRepo(&domain.Agent{ID: "db-1", Name: "go-developer", Description: "stale db copy"})
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer", Description: "live config"})
	merged := MergeAgents(db, cfg)

	got := merged.List()
	if len(got) != 1 {
		t.Fatalf("expected the collision to collapse to 1 entry, got %d: %+v", len(got), got)
	}
	if got[0].Description != "live config" {
		t.Fatalf("expected config to win, got %+v", got[0])
	}
}

func TestMergedAgentRepository_GetRoutesByIDPrefix(t *testing.T) {
	db := newFakeAgentRepo(&domain.Agent{ID: "db-1", Name: "designer"})
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer"})
	merged := MergeAgents(db, cfg)

	if a := merged.Get("db-1"); a == nil || a.Name != "designer" {
		t.Fatalf("expected db-1 routed to db, got %+v", a)
	}
	if a := merged.Get("cfg:go-developer"); a == nil || a.Name != "go-developer" {
		t.Fatalf("expected cfg:go-developer routed to cfg, got %+v", a)
	}
}

func TestMergedAgentRepository_WritesGoToDB(t *testing.T) {
	db := newFakeAgentRepo()
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer"})
	merged := MergeAgents(db, cfg)

	if _, err := merged.Create("x", "", "", nil, nil); err != nil || !db.created {
		t.Fatalf("expected Create to reach db, err=%v created=%v", err, db.created)
	}
}

func TestMergedAgentRepository_UpdateOnConfigIDRefused(t *testing.T) {
	db := newFakeAgentRepo()
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer"})
	merged := MergeAgents(db, cfg)

	if _, err := merged.Update("cfg:go-developer", "x", "", "", nil, nil); err == nil {
		t.Fatal("expected Update on a config-sourced id to be refused")
	}
	if db.updated {
		t.Fatal("expected the db not to be touched")
	}
}

func TestMergedAgentRepository_DeleteOnConfigIDRefused(t *testing.T) {
	db := newFakeAgentRepo()
	cfg := newFakeAgentRepo(&domain.Agent{ID: "cfg:go-developer", Name: "go-developer"})
	merged := MergeAgents(db, cfg)

	if err := merged.Delete("cfg:go-developer"); err == nil {
		t.Fatal("expected Delete on a config-sourced id to be refused")
	}
	if db.deleted {
		t.Fatal("expected the db not to be touched")
	}
}

// fakeSkillRepo mirrors fakeAgentRepo for skill.Store.
type fakeSkillRepo struct{ byID map[string]*skill.Skill }

func newFakeSkillRepo(skills ...*skill.Skill) *fakeSkillRepo {
	r := &fakeSkillRepo{byID: map[string]*skill.Skill{}}
	for _, s := range skills {
		r.byID[s.ID] = s
	}
	return r
}
func (r *fakeSkillRepo) Get(id string) *skill.Skill { return r.byID[id] }
func (r *fakeSkillRepo) List() []*skill.Skill {
	out := make([]*skill.Skill, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, s)
	}
	return out
}
func (r *fakeSkillRepo) Create(skill.Input) (*skill.Skill, error) {
	return &skill.Skill{ID: "db-new"}, nil
}
func (r *fakeSkillRepo) Update(id string, _ skill.Input) (*skill.Skill, error) {
	return r.byID[id], nil
}
func (r *fakeSkillRepo) Delete(string) error { return nil }

func (r *fakeSkillRepo) ListFiles(skillID string) ([]skill.File, error) {
	if s := r.byID[skillID]; s != nil {
		return s.Files, nil
	}
	return nil, nil
}
func (r *fakeSkillRepo) PutFile(string, skill.File) error                 { return nil }
func (r *fakeSkillRepo) RenameFile(string, string, string) error          { return nil }
func (r *fakeSkillRepo) DeleteFile(string, string) error                  { return nil }
func (r *fakeSkillRepo) SetPublishState(string, skill.PublishState) error { return nil }

func TestMergedSkillStore_ConfigWinsOnNameCollision(t *testing.T) {
	db := newFakeSkillRepo(&skill.Skill{ID: "db-1", Name: "tdd", Description: "stale"})
	cfg := newFakeSkillRepo(&skill.Skill{ID: "cfg:tdd", Name: "tdd", Description: "live"})
	merged := MergeSkills(db, cfg)

	got := merged.List()
	if len(got) != 1 || got[0].Description != "live" {
		t.Fatalf("expected config to win, got %+v", got)
	}
}

func TestMergedSkillStore_UpdateOnConfigIDRefused(t *testing.T) {
	db := newFakeSkillRepo()
	cfg := newFakeSkillRepo(&skill.Skill{ID: "cfg:tdd", Name: "tdd"})
	merged := MergeSkills(db, cfg)
	if _, err := merged.Update("cfg:tdd", skill.Input{}); err == nil {
		t.Fatal("expected Update on a config-sourced id to be refused")
	}
}

// fakeMCPServerRepo mirrors fakeAgentRepo for mcpserver.Store.
type fakeMCPServerRepo struct{ byID map[string]*mcpserver.Server }

func newFakeMCPServerRepo(servers ...*mcpserver.Server) *fakeMCPServerRepo {
	r := &fakeMCPServerRepo{byID: map[string]*mcpserver.Server{}}
	for _, s := range servers {
		r.byID[s.ID] = s
	}
	return r
}
func (r *fakeMCPServerRepo) Get(id string) *mcpserver.Server { return r.byID[id] }
func (r *fakeMCPServerRepo) List() []*mcpserver.Server {
	out := make([]*mcpserver.Server, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, s)
	}
	return out
}
func (r *fakeMCPServerRepo) Create(mcpserver.Input) (*mcpserver.Server, error) {
	return &mcpserver.Server{ID: "db-new"}, nil
}
func (r *fakeMCPServerRepo) Update(id string, _ mcpserver.Input) (*mcpserver.Server, error) {
	return r.byID[id], nil
}
func (r *fakeMCPServerRepo) UpdateProbeResult(id string, _ mcpserver.ProbeResult) (*mcpserver.Server, error) {
	return r.byID[id], nil
}
func (r *fakeMCPServerRepo) Delete(string) error { return nil }

func TestMergedMCPServerStore_ConfigWinsOnNameCollision(t *testing.T) {
	db := newFakeMCPServerRepo(&mcpserver.Server{ID: "db-1", Name: "postgres", Command: "stale"})
	cfg := newFakeMCPServerRepo(&mcpserver.Server{ID: "cfg:postgres", Name: "postgres", Command: "live"})
	merged := MergeMCPServers(db, cfg)

	got := merged.List()
	if len(got) != 1 || got[0].Command != "live" {
		t.Fatalf("expected config to win, got %+v", got)
	}
}

func TestMergedMCPServerStore_DeleteOnConfigIDRefused(t *testing.T) {
	db := newFakeMCPServerRepo()
	cfg := newFakeMCPServerRepo(&mcpserver.Server{ID: "cfg:postgres", Name: "postgres"})
	merged := MergeMCPServers(db, cfg)
	if err := merged.Delete("cfg:postgres"); err == nil {
		t.Fatal("expected Delete on a config-sourced id to be refused")
	}
}
