package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// deliveryFiles are a repository's instruction files, in memory: the source
// ListInstructionFiles of the brief's architecture source.
func (a architectureSource) InstructionFilesOf(_ context.Context, id string) ([]domain.InstructionFile, error) {
	return a.files[id], nil
}

func instructionFile(path, kind, content string) domain.InstructionFile {
	return domain.InstructionFile{RepositoryID: "r1", Path: path, Kind: kind, Content: content, Version: 1}
}

func withInstructions(files ...domain.InstructionFile) architectureSource {
	src := widgetsArchitecture()
	src.files = map[string][]domain.InstructionFile{"r1": files}
	return src
}

func delivered(t *testing.T, src ports.ArchitectureBriefSource, role domain.SessionRole, agent, repo string) map[string]llmkit.InstructionFile {
	t.Helper()
	var ag *domain.Agent
	if agent != "" {
		ag = &domain.Agent{Name: agent}
	}
	out := map[string]llmkit.InstructionFile{}
	for _, f := range (&Service{Architectures: src}).instructionFiles(context.Background(), role, ag, "p1", repo) {
		out[f.Path] = f
	}
	return out
}

func TestInstructionDelivery_TheStoredFilesGoWithTheirKindsAndTheArchitectureSummary(t *testing.T) {
	src := withInstructions(
		instructionFile("CLAUDE.md", "memory", "root memory"),
		instructionFile("web/AGENTS.md", "memory", "web memory"),
		instructionFile(".claude/skills/review/SKILL.md", "skill", "skill"),
		instructionFile(".claude/skills/review/notes.md", "skill", "notes"),
		instructionFile(".claude/agents/reviewer.md", "subagent", "reviewer"),
	)
	got := delivered(t, src, domain.RoleDelegate, "go-developer", "r1")
	want := map[string]llmkit.InstructionKind{
		"CLAUDE.md": llmkit.InstructionMemory, "web/AGENTS.md": llmkit.InstructionMemory,
		".claude/skills/review/SKILL.md": llmkit.InstructionSkill, ".claude/skills/review/notes.md": llmkit.InstructionSkill,
		".claude/agents/reviewer.md": llmkit.InstructionSubAgent, "ARCHITECTURE.md": llmkit.InstructionMemory,
	}
	if len(got) != len(want) {
		t.Errorf("delivered %d files: %v", len(got), got)
	}
	for p, kind := range want {
		if f, ok := got[p]; !ok || f.Kind != kind {
			t.Errorf("%s = %+v, want kind %s", p, f, kind)
		}
	}
	if string(got["CLAUDE.md"].Content) != "root memory" {
		t.Errorf("content = %q", got["CLAUDE.md"].Content)
	}
	// The summary says what the old brief section said.
	summary := string(got["ARCHITECTURE.md"].Content)
	for _, want := range []string{"You work as go-developer", "domain, ports", "You may not read these layers: secrets", "Architecture rules guard your edits"} {
		if !strings.Contains(summary, want) {
			t.Errorf("ARCHITECTURE.md lacks %q:\n%s", want, summary)
		}
	}
}

func TestInstructionDelivery_AppliesToKeepsAFileFromAgentsOutsideIt(t *testing.T) {
	src := withInstructions(
		instructionFile("CLAUDE.md", "memory", "everyone"),
		instructionFile("AGENTS.md", "memory", "architects only"),
		instructionFile("GEMINI.md", "memory", "no entry: everyone"),
	)
	src.records["r1"] = &domain.ArchitectureRecord{
		Instructions: []domain.ArchInstruction{
			{Path: "CLAUDE.md", Kind: "memory", AppliesTo: []string{"all"}},
			{Path: "AGENTS.md", Kind: "memory", AppliesTo: []string{"software-architect"}},
		},
		Unlayered: domain.LayerAccess{Default: domain.AccessRead},
	}
	dev := delivered(t, src, domain.RoleDelegate, "go-developer", "r1")
	if _, ok := dev["AGENTS.md"]; ok || dev["CLAUDE.md"].Path == "" || dev["GEMINI.md"].Path == "" {
		t.Errorf("go-developer got %v", dev)
	}
	arch := delivered(t, src, domain.RoleDelegate, "software-architect", "r1")
	if arch["AGENTS.md"].Path == "" {
		t.Errorf("software-architect got %v", arch)
	}
	plain := delivered(t, src, domain.RolePeer, "", "r1")
	if _, ok := plain["AGENTS.md"]; ok {
		t.Errorf("a plain session got an architect's file: %v", plain)
	}
}

func TestInstructionDelivery_NoRecordDeliversTheFilesWithoutASummary(t *testing.T) {
	src := withInstructions(instructionFile("CLAUDE.md", "memory", "memory"))
	delete(src.records, "r1")
	src.refs = nil
	got := delivered(t, src, domain.RoleDelegate, "go-developer", "r1")
	if len(got) != 1 || got["CLAUDE.md"].Path == "" {
		t.Errorf("delivered %v", got)
	}
}

func TestInstructionDelivery_NothingStoredAndNoRecordDeliversNothing(t *testing.T) {
	if got := delivered(t, architectureSource{}, domain.RoleDelegate, "go-developer", "r1"); len(got) != 0 {
		t.Errorf("delivered %v", got)
	}
	if got := delivered(t, nil, domain.RoleDelegate, "go-developer", "r1"); len(got) != 0 {
		t.Errorf("no source: %v", got)
	}
}

func TestInstructionDelivery_ASkillWithoutItsSkillMdIsLeftOutSoTheStartIsNotRefused(t *testing.T) {
	src := withInstructions(
		instructionFile(".claude/skills/broken/notes.md", "skill", "no SKILL.md here"),
		instructionFile(".claude/skills/ok/SKILL.md", "skill", "ok"),
		instructionFile(".claude/skills/ok/notes.md", "skill", "notes"),
	)
	got := delivered(t, src, domain.RoleDelegate, "go-developer", "r1")
	if _, ok := got[".claude/skills/broken/notes.md"]; ok {
		t.Errorf("a skill folder without SKILL.md was delivered: %v", got)
	}
	if got[".claude/skills/ok/SKILL.md"].Path == "" || got[".claude/skills/ok/notes.md"].Path == "" {
		t.Errorf("a whole skill was dropped: %v", got)
	}
}

func TestInstructionDelivery_AnArchitectGetsTheProjectSummaryAndNoRepositoryFiles(t *testing.T) {
	src := withInstructions(instructionFile("CLAUDE.md", "memory", "memory"))
	got := delivered(t, src, domain.RoleArchitect, "software-architect", "")
	if len(got) != 1 || !strings.Contains(string(got["ARCHITECTURE.md"].Content), "project/architecture/widgets.yaml") {
		t.Errorf("delivered %v", got)
	}
}

func TestInstructionDelivery_BothModesCarryTheFilesAndTheSessionsEnvIsLeftAlone(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Architectures = withInstructions(instructionFile("CLAUDE.md", "memory", "memory"))
	cfg := svc.sessionConfig("s1", t.TempDir(), "", nil, llmkit.PermissionAsk, resolvedAgent{}, sessionSubject{ticket: &domain.Ticket{ID: "tk1", ProjectID: "p1", Title: "T"}}, "", domain.RolePeer, "r1")
	if len(cfg.InstructionFiles) != 2 {
		t.Fatalf("headless config carries %v", cfg.InstructionFiles)
	}
	for _, kv := range cfg.Env {
		if strings.HasPrefix(kv, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD") {
			t.Errorf("the harness set %s; llmkit owns it", kv)
		}
	}
	spec := svc.agentSpec(cfg, ports.Conversation{ID: "s1"}, "go")
	if len(spec.InstructionFiles) != 2 {
		t.Fatalf("interactive spec carries %v", spec.InstructionFiles)
	}
	for _, f := range spec.InstructionFiles {
		if f.Path == "CLAUDE.md" && (f.Kind != "memory" || string(f.Content) != "memory") {
			t.Errorf("spec file = %+v", f)
		}
	}
}

func TestInstructionDelivery_ADriverRefusalFailsTheStartAndNamesThePath(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Architectures = withInstructions(instructionFile("web/../CLAUDE.md", "memory", "memory"))
	_, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "tk1"})
	if err == nil || !strings.Contains(err.Error(), "web/../CLAUDE.md") || !errors.Is(err, llmkit.ErrInvalidInstructions) {
		t.Fatalf("Start = %v, want ErrInvalidInstructions naming the path", err)
	}
}

func TestInstructionDelivery_AHeadlessStartHandsTheFilesToTheDriver(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Architectures = withInstructions(instructionFile("CLAUDE.md", "memory", "memory"))
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello", TicketID: "tk1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	run, ok := svc.runtime.Get(d.ID)
	if !ok {
		t.Fatal("session not running")
	}
	files := run.Config().InstructionFiles
	if len(files) != 2 || files[1].Path != "CLAUDE.md" {
		t.Errorf("the driver was given %v", files)
	}
	// A save after the start changes nothing the session holds.
	svc.Architectures = withInstructions(instructionFile("CLAUDE.md", "memory", "changed"))
	if got := run.Config().InstructionFiles; string(got[1].Content) != "memory" {
		t.Errorf("the running session's set changed: %q", got[1].Content)
	}
}
