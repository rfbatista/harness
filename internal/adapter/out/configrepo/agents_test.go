package configrepo

import (
	"path/filepath"
	"reflect"
	"testing"
)

func newFixtureAgentRepo(t *testing.T) (*AgentRepository, string) {
	t.Helper()
	agentsRoot := t.TempDir()
	writeSkill(t, filepath.Join(agentsRoot, "skills"), "tdd", tddSkillMD)
	writeSkill(t, filepath.Join(agentsRoot, "skills"), "git-commit",
		"---\nname: git-commit\ndescription: x\n---\n\nbody\n")
	writeMCPJSON(t, agentsRoot, "backend-developer", backendMCPJSON)

	manifest := Manifest{
		Bundles: map[string][]string{"developer": {"tdd", "git-commit"}},
		Agents: map[string]AgentConfig{
			"go-developer":      {Skills: []string{"@developer"}, Description: "Go"},
			"backend-developer": {Skills: []string{"tdd"}, MCP: "self"},
		},
	}
	manifestPath := writeManifest(t, agentsRoot, manifest)
	mcpStore := NewMCPServerStore(manifestPath, agentsRoot)
	return NewAgentRepository(manifestPath, mcpStore), agentsRoot
}

func TestAgentRepository_ListBuildsEveryAgentWithResolvedSkillIDs(t *testing.T) {
	repo, _ := newFixtureAgentRepo(t)
	got := repo.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.Name == "go-developer" {
			want := []string{"cfg:tdd", "cfg:git-commit"}
			if !reflect.DeepEqual(a.SkillIDs, want) {
				t.Fatalf("go-developer SkillIDs = %v, want %v", a.SkillIDs, want)
			}
			if a.ID != "cfg:go-developer" || a.Description != "Go" {
				t.Fatalf("go-developer = %+v", a)
			}
		}
	}
}

func TestAgentRepository_ResolvesMCPServerIDs(t *testing.T) {
	repo, _ := newFixtureAgentRepo(t)
	a := repo.Get("cfg:backend-developer")
	if a == nil {
		t.Fatal("expected backend-developer to resolve")
	}
	want := map[string]bool{"cfg:postgres": true, "cfg:github": true}
	if len(a.MCPServerIDs) != 2 {
		t.Fatalf("MCPServerIDs = %v", a.MCPServerIDs)
	}
	for _, id := range a.MCPServerIDs {
		if !want[id] {
			t.Fatalf("unexpected mcp server id %s in %v", id, a.MCPServerIDs)
		}
	}
}

func TestAgentRepository_GetReturnsNilForUnknownAgent(t *testing.T) {
	repo, _ := newFixtureAgentRepo(t)
	if a := repo.Get("cfg:nope"); a != nil {
		t.Fatalf("expected nil, got %+v", a)
	}
}

func TestAgentRepository_GetReturnsNilForNonConfigID(t *testing.T) {
	repo, _ := newFixtureAgentRepo(t)
	if a := repo.Get("db-uuid"); a != nil {
		t.Fatalf("expected nil for a non-\"cfg:\" id, got %+v", a)
	}
}

func TestAgentRepository_WritesAreReadOnly(t *testing.T) {
	repo, _ := newFixtureAgentRepo(t)
	if _, err := repo.Create("x", "", "", nil, nil); err == nil {
		t.Fatal("expected Create to fail")
	}
	if _, err := repo.Update("cfg:go-developer", "x", "", "", nil, nil); err == nil {
		t.Fatal("expected Update to fail")
	}
	if err := repo.Delete("cfg:go-developer"); err == nil {
		t.Fatal("expected Delete to fail")
	}
}
