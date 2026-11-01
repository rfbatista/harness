package orchestration

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// repositoriesByID is the catalog's repositories, in memory.
type repositoriesByID map[string]*domain.Repository

func (r repositoriesByID) GetRepository(_ context.Context, id string) (*domain.Repository, error) {
	if repo, ok := r[id]; ok {
		return repo, nil
	}
	return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
}

func (r repositoriesByID) ListRepositories(context.Context, string) ([]*domain.Repository, error) {
	return nil, nil
}

type noMCPServers struct{}

func (noMCPServers) ListMCPServers() []*domain.MCPServer { return nil }

var applied = "ds1"

// designSystemNames names the applied design systems by id.
type designSystemNames map[string]string

func (n designSystemNames) GetDesignSystem(_ context.Context, id string) (*domain.DesignSystem, error) {
	if name, ok := n[id]; ok {
		return &domain.DesignSystem{ID: id, Name: name}, nil
	}
	return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "design system not found"}
}

func designSystemService() *Service {
	return &Service{catalog: Catalog{MCPServers: noMCPServers{}, DesignSystems: designSystemNames{"ds1": "Brand One"}, Repositories: repositoriesByID{
		"opted":  {ID: "opted", DesignSystemID: &applied, DesignSystemPath: ".harness/design-system/"},
		"plain":  {ID: "plain"},
		"movedn": {ID: "movedn", DesignSystemPath: "ui/"},
	}}}
}

// briefFor is the session config's brief, as a session of role in repository
// starts with it.
func briefFor(s *Service, role domain.SessionRole, repository string) string {
	ag := resolvedAgent{agent: &domain.Agent{Name: "go-developer"}}
	subject := sessionSubject{project: &domain.Project{ID: "p1"}, ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1", Title: "Ship the thing"}}
	cfg := s.sessionConfig("s1", "/tmp", "", nil, "", ag, subject, "", role, repository)
	return cfg.AppendSystem
}

func TestDesignSystemBrief_PointerIsTheContractsTextInAtMostFiveLines(t *testing.T) {
	got := designSystemSection("Brand One", &domain.Repository{DesignSystemID: &applied})
	checkGolden(t, "design_system_brief", got)
	if n := len(strings.Split(strings.TrimSpace(got), "\n")); n > 5 {
		t.Errorf("%d lines, want at most 5", n)
	}
}

func TestDesignSystemBrief_OptedInSessionsOfEveryRoleGetItAfterTheirBrief(t *testing.T) {
	s := designSystemService()
	pointer := designSystemSection("Brand One", &domain.Repository{DesignSystemID: &applied})
	for name, role := range map[string]domain.SessionRole{"delegate": domain.RoleDelegate, "plain": domain.RolePeer, "architect": domain.RoleArchitect} {
		base := briefFor(s, role, "plain")
		got := briefFor(s, role, "opted")
		if want := appendSection(base, pointer); got != want {
			t.Errorf("%s: brief with the pointer differs from the brief plus the section:\n%s", name, got)
		}
		if strings.Count(got, `This repository uses the project design system "Brand One". `) != 1 {
			t.Errorf("%s: pointer not exactly once", name)
		}
	}
}

func TestDesignSystemBrief_BriefsOfEveryRoleNameTheDesignSystem(t *testing.T) {
	s := designSystemService()
	for name, role := range map[string]domain.SessionRole{"delegate": domain.RoleDelegate, "plain": domain.RolePeer, "architect": domain.RoleArchitect} {
		checkGolden(t, "design_system_brief_"+name, briefFor(s, role, "opted"))
	}
}

func TestDesignSystemBrief_OthersGetNothing(t *testing.T) {
	s := designSystemService()
	for _, repo := range []string{"plain", "movedn", "unknown", ""} {
		if got := briefFor(s, domain.RoleDelegate, repo); strings.Contains(got, "project design system") {
			t.Errorf("repository %q: %s", repo, got)
		}
	}
	if got := briefFor(&Service{catalog: Catalog{MCPServers: noMCPServers{}}}, domain.RoleDelegate, "opted"); strings.Contains(got, "project design system") {
		t.Errorf("no catalog: %s", got)
	}
}

var _ ports.RepositoryReader = repositoriesByID(nil)
