package backend

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func TestFakeProjectLifecycle(t *testing.T) {
	f := NewFake()
	p, err := f.CreateProject("demo", "/src/demo")
	if err != nil || p.ID == "" || len(f.Projects) != 1 {
		t.Fatalf("create: %+v %v", p, err)
	}
	if _, err := f.UpdateProject(p.ID, "demo2", "/src/demo2"); err != nil || f.Projects[0].Name != "demo2" {
		t.Fatalf("update: %v %+v", err, f.Projects[0])
	}
	if _, err := f.AddIgnoredPath(p.ID, "node_modules"); err != nil || len(f.Projects[0].IgnoredPaths) != 1 {
		t.Fatalf("ignored: %v", err)
	}
	if err := f.DeleteProject(p.ID); err != nil || len(f.Projects) != 0 {
		t.Fatalf("delete: %v", err)
	}
	if err := f.DeleteProject("nope"); errs.Code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing project should be PROJECT_NOT_FOUND, got %v", err)
	}
}

func TestFakeCreateProjectRequiresName(t *testing.T) {
	f := NewFake()
	if _, err := f.CreateProject("", "/x"); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("want INVALID_INPUT got %v", err)
	}
}

func TestFakeRepositoryAndBoundedContextScopedToProject(t *testing.T) {
	f := NewFake()
	p, _ := f.CreateProject("demo", "/src")
	if _, err := f.CreateRepository("missing", "r", "", "", "/r"); errs.Code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("repo on missing project: %v", err)
	}
	r, err := f.CreateRepository(p.ID, "main", "", "", "/src")
	if err != nil || len(f.ListRepositories(p.ID)) != 1 {
		t.Fatalf("repo: %v", err)
	}
	if err := f.DeleteRepository(r.ID); err != nil || len(f.Repositories) != 0 {
		t.Fatalf("delete repo: %v", err)
	}
	bc, err := f.CreateBoundedContext(p.ID, "Billing", "money", []domain.LanguageTerm{{Term: "Invoice", Definition: "bill"}})
	if err != nil || len(f.ListBoundedContexts(p.ID)) != 1 {
		t.Fatalf("bc: %v", err)
	}
	if _, err := f.UpdateBoundedContext(bc.ID, "Billing2", "money", nil); err != nil || f.BoundedContexts[0].Name != "Billing2" {
		t.Fatalf("update bc: %v", err)
	}
	if err := f.DeleteBoundedContext(bc.ID); err != nil || len(f.BoundedContexts) != 0 {
		t.Fatalf("delete bc: %v", err)
	}
}

func TestFakeAgentLifecycle(t *testing.T) {
	f := NewFake()
	a, err := f.CreateAgent("reviewer", "reviews", "", []string{"k1"}, nil)
	if err != nil || len(f.Agents) != 1 {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.UpdateAgent(a.ID, "reviewer", "reviews", "", []string{"k1", "k2"}, []string{"m1"}); err != nil || len(f.Agents[0].SkillIDs) != 2 {
		t.Fatalf("update: %v", err)
	}
	if err := f.DeleteAgent("nope"); errs.Code(err) != "AGENT_NOT_FOUND" {
		t.Fatalf("want AGENT_NOT_FOUND got %v", err)
	}
	if err := f.DeleteAgent(a.ID); err != nil || len(f.Agents) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestFakeSkillFiles(t *testing.T) {
	f := NewFake()
	s, err := f.CreateSkill(domain.SkillInput{Name: "tdd", Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# tdd"}}})
	if err != nil || len(f.Skills) != 1 {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.PutSkillFile(s.ID, domain.SkillFile{Path: "ref.md", Content: "x"}); err != nil || len(f.Skills[0].Files) != 2 {
		t.Fatalf("put: %v", err)
	}
	if _, err := f.PutSkillFile(s.ID, domain.SkillFile{Path: "ref.md", Content: "y"}); err != nil || len(f.Skills[0].Files) != 2 || f.Skills[0].Files[1].Content != "y" {
		t.Fatalf("put overwrites in place: %v %+v", err, f.Skills[0].Files)
	}
	if _, err := f.RenameSkillFile(s.ID, "ref.md", "docs/ref.md"); err != nil || f.Skills[0].Files[1].Path != "docs/ref.md" {
		t.Fatalf("rename: %v", err)
	}
	if _, err := f.DeleteSkillFile(s.ID, "docs/ref.md"); err != nil || len(f.Skills[0].Files) != 1 {
		t.Fatalf("delete file: %v", err)
	}
	if _, err := f.DeleteSkillFile(s.ID, "missing"); errs.Code(err) != "SKILL_FILE_NOT_FOUND" {
		t.Fatalf("want SKILL_FILE_NOT_FOUND got %v", err)
	}
	got := f.GetSkill(s.ID)
	if got == nil || got.Files[0].Content != "# tdd" {
		t.Fatalf("get should return file content: %+v", got)
	}
	if _, err := f.PublishSkill(s.ID, false); err != nil || f.Skills[0].PublishedPath == "" {
		t.Fatalf("publish: %v", err)
	}
	if _, err := f.PublishSkill(s.ID, false); errs.Code(err) != "PUBLISH_TARGET_EXISTS" {
		t.Fatalf("second publish without force: %v", err)
	}
	if _, err := f.PublishSkill(s.ID, true); err != nil {
		t.Fatalf("force publish: %v", err)
	}
	if _, err := f.UnpublishSkill(s.ID); err != nil || f.Skills[0].PublishedPath != "" {
		t.Fatalf("unpublish: %v", err)
	}
	if err := f.DeleteSkill(s.ID); err != nil || len(f.Skills) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestFakeSkillImportUsesValidationHook(t *testing.T) {
	f := NewFake()
	f.ValidatePath = func(path string) (*domain.SkillPathValidation, error) {
		return &domain.SkillPathValidation{Valid: true, SkillRoot: path, Preview: &domain.Skill{Name: "imported"}}, nil
	}
	v, err := f.ValidateSkillPath("/skills/x")
	if err != nil || !v.Valid {
		t.Fatalf("validate: %v", err)
	}
	s, err := f.ImportSkillFromPath("/skills/x")
	if err != nil || s.Name != "imported" || len(f.Skills) != 1 {
		t.Fatalf("import: %v %+v", err, s)
	}
}

func TestFakeMCPServerLifecycleAndProbe(t *testing.T) {
	f := NewFake()
	s, err := f.CreateMCPServer(domain.MCPServerInput{Name: "fs", Transport: domain.MCPTransportStdio, Command: "npx"})
	if err != nil || len(f.MCPServers) != 1 {
		t.Fatalf("create: %v", err)
	}
	f.Probe = func(id string) (domain.MCPProbeResult, error) {
		return domain.MCPProbeResult{Status: domain.MCPProbeStatusOK, ToolCount: 3}, nil
	}
	res, err := f.ProbeMCPServer(context.Background(), s.ID)
	if err != nil || res.ToolCount != 3 || f.MCPServers[0].ToolCount != 3 || f.MCPServers[0].LastProbeStatus != domain.MCPProbeStatusOK {
		t.Fatalf("probe should persist: %v %+v", err, f.MCPServers[0])
	}
	if _, err := f.UpdateMCPServer(s.ID, domain.MCPServerInput{Name: "fs2", Transport: domain.MCPTransportStdio}); err != nil || f.MCPServers[0].Name != "fs2" {
		t.Fatalf("update: %v", err)
	}
	if err := f.DeleteMCPServer(s.ID); err != nil || len(f.MCPServers) != 0 {
		t.Fatalf("delete: %v", err)
	}
}

func TestFakeImportMCPServersParsesCursorFormat(t *testing.T) {
	f := NewFake()
	content := `{"mcpServers": {"fs": {"command": "npx", "args": ["-y", "fs"]}, "web": {"url": "http://localhost:9/mcp"}}}`
	got, err := f.ImportMCPServers(content, "skip")
	if err != nil || len(got) != 2 || len(f.MCPServers) != 2 {
		t.Fatalf("import: %v %d", err, len(got))
	}
	if _, err := f.ImportMCPServers(content, "bogus"); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("bad policy: %v", err)
	}
}

func TestFakeUpdateSettingsMerges(t *testing.T) {
	f := NewFake()
	f.Settings["a"] = "1"
	got, err := f.UpdateSettings(map[string]string{"b": "2"})
	if err != nil || got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("merge: %v %v", err, got)
	}
}
