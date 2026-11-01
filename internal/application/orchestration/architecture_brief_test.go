package orchestration

import (
	"context"
	"os"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// architectureSource is a project's saved architecture records, in memory.
type architectureSource struct {
	refs    []ports.ArchitectureRef
	records map[string]*domain.ArchitectureRecord
	files   map[string][]domain.InstructionFile
}

func (a architectureSource) ListArchitectureRefs(context.Context, string) ([]ports.ArchitectureRef, error) {
	return a.refs, nil
}

func (a architectureSource) SavedArchitecture(_ context.Context, id string) (*domain.ArchitectureRecord, error) {
	return a.records[id], nil
}

func layer(name string, access domain.LayerAccess) domain.ArchLayer {
	return domain.ArchLayer{Name: name, Match: domain.ArchLayerMatch{Folder: "internal/" + name}, Access: access}
}

func levels(def domain.AccessLevel, agents map[string]domain.AccessLevel) domain.LayerAccess {
	return domain.LayerAccess{Default: def, Agents: agents}
}

// widgetsArchitecture: domain and ports are go-developer's, adapters are the
// architect's and read by go-developer, secrets are read by nobody but the
// architect.
func widgetsArchitecture() architectureSource {
	rec := &domain.ArchitectureRecord{
		Layers: []domain.ArchLayer{
			layer("domain", levels(domain.AccessRead, map[string]domain.AccessLevel{"go-developer": domain.AccessChange, "software-architect": domain.AccessChange})),
			layer("ports", levels(domain.AccessRead, map[string]domain.AccessLevel{"go-developer": domain.AccessChange, "software-architect": domain.AccessChange})),
			layer("adapters", levels(domain.AccessRead, map[string]domain.AccessLevel{"software-architect": domain.AccessChange})),
			layer("secrets", levels(domain.AccessNone, map[string]domain.AccessLevel{"software-architect": domain.AccessChange})),
		},
		Unlayered: levels(domain.AccessRead, map[string]domain.AccessLevel{"software-architect": domain.AccessChange, "devops": domain.AccessChange}),
	}
	return architectureSource{
		refs: []ports.ArchitectureRef{
			{RepositoryID: "r1", Repository: "widgets", Path: "project/architecture/widgets.yaml"},
			{RepositoryID: "r2", Repository: "docs site", Path: "project/architecture/docs-site.yaml"},
		},
		records: map[string]*domain.ArchitectureRecord{"r1": rec, "r2": rec},
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := "testdata/" + name + ".golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s changed:\n%s", name, got)
	}
}

func section(t *testing.T, src ports.ArchitectureBriefSource, role domain.SessionRole, agent string, repo string) string {
	t.Helper()
	s := &Service{Architectures: src}
	var ag *domain.Agent
	if agent != "" {
		ag = &domain.Agent{Name: agent}
	}
	return s.architectureSummary(context.Background(), role, ag, "p1", repo)
}

func brief(t *testing.T, src ports.ArchitectureBriefSource, role domain.SessionRole, agent string, repo string) string {
	t.Helper()
	s := &Service{Architectures: src}
	var ag *domain.Agent
	if agent != "" {
		ag = &domain.Agent{Name: agent}
	}
	return s.architectureSection(context.Background(), role, ag, "p1", repo)
}

// The brief says one thing about the architecture: where it is. The layers,
// access and rules are in the delivered ARCHITECTURE.md (ADR 0057).
func TestArchitectureBrief_IsOnePointerLine(t *testing.T) {
	src := widgetsArchitecture()
	checkGolden(t, "architecture_brief_delegate", brief(t, src, domain.RoleDelegate, "go-developer", "r1"))
	checkGolden(t, "architecture_brief_plain", brief(t, src, domain.RolePeer, "", "r1"))
	checkGolden(t, "architecture_brief_architect", brief(t, src, domain.RoleArchitect, "software-architect", "r1"))
	for _, got := range []string{brief(t, src, domain.RoleDelegate, "go-developer", "r1"), brief(t, src, domain.RoleArchitect, "software-architect", "r1")} {
		if strings.Contains(got, "domain") || strings.Contains(got, "secrets") || strings.Contains(got, "blocking") {
			t.Errorf("the brief lists layers or rules:\n%s", got)
		}
	}
}

func TestArchitectureBrief_NoRecordNoPointer(t *testing.T) {
	if got := brief(t, widgetsArchitecture(), domain.RoleDelegate, "go-developer", "r-without-record"); got != "" {
		t.Errorf("a repository with no record: %q", got)
	}
	if got := brief(t, architectureSource{}, domain.RoleArchitect, "software-architect", ""); got != "" {
		t.Errorf("no record in the project: %q", got)
	}
}

func TestArchitectureBrief_DelegateNamesItsLayersAndWhatItMayNotRead(t *testing.T) {
	checkGolden(t, "architecture_summary_delegate", section(t, widgetsArchitecture(), domain.RoleDelegate, "go-developer", "r1"))
}

func TestArchitectureBrief_PlainSessionIsAnAgentToo(t *testing.T) {
	checkGolden(t, "architecture_summary_plain", section(t, widgetsArchitecture(), domain.RolePeer, "", "r1"))
}

func TestArchitectureBrief_AgentThatMayChangeEverythingSaysEveryLayer(t *testing.T) {
	got := section(t, widgetsArchitecture(), domain.RoleDelegate, "software-architect", "r1")
	checkGolden(t, "architecture_summary_every_layer", got)
}

func TestArchitectureBrief_ArchitectListsEveryRecordAndNeverEditsOne(t *testing.T) {
	checkGolden(t, "architecture_summary_architect", section(t, widgetsArchitecture(), domain.RoleArchitect, "software-architect", "r1"))
}

func TestArchitectureBrief_NoRecordNoSection(t *testing.T) {
	src := widgetsArchitecture()
	if got := section(t, src, domain.RoleDelegate, "go-developer", "r-without-record"); got != "" {
		t.Errorf("a repository with no record: %q", got)
	}
	if got := section(t, architectureSource{}, domain.RoleArchitect, "software-architect", "r1"); got != "" {
		t.Errorf("an architect, no record in the project: %q", got)
	}
	if got := section(t, nil, domain.RoleDelegate, "go-developer", "r1"); got != "" {
		t.Errorf("no source: %q", got)
	}
}
