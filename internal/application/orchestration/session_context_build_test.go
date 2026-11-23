package orchestration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

func TestPieceTrail_EachMarkTakesTheTextAppendedSinceTheLast(t *testing.T) {
	cfg := llmkit.SessionConfig{AppendSystem: "agent says hi\n"}
	trail := &pieceTrail{}
	trail.mark(&cfg, domain.PieceRepositoryPrompt, "Repository prompt", "Repository harness")
	cfg.AppendSystem = appendSection(cfg.AppendSystem, "## Your task\n\nDo it.\n\nTwice.")
	trail.mark(&cfg, domain.PieceBrief, "Task brief", "Harness")
	trail.mark(&cfg, domain.PieceOther, "Nothing added", "Harness") // adds nothing: no piece
	cfg.AppendSystem = appendSection(cfg.AppendSystem, "## Services\n\nrun them")
	trail.mark(&cfg, domain.PieceOther, "Services", "Harness")

	got := trail.pieces
	if len(got) != 3 {
		t.Fatalf("pieces = %+v", got)
	}
	want := []struct{ id, kind, text string }{
		{"repository-prompt", domain.PieceRepositoryPrompt, "agent says hi\n"},
		{"task-brief", domain.PieceBrief, "## Your task\n\nDo it.\n\nTwice."},
		{"services", domain.PieceOther, "## Services\n\nrun them"},
	}
	for i, w := range want {
		if got[i].ID != w.id || got[i].Kind != w.kind || got[i].Text != w.text {
			t.Errorf("piece %d = %+v, want %+v", i, got[i], w)
		}
	}
	// Joined with the blank line appendSection puts between them, the pieces are the system prompt.
	var texts []string
	for _, p := range got {
		texts = append(texts, strings.TrimRight(p.Text, "\n"))
	}
	if strings.Join(texts, "\n\n") != cfg.AppendSystem {
		t.Errorf("pieces do not add up to the system prompt")
	}
}

func TestPieceTrail_ANilTrailMarksNothing(t *testing.T) {
	var trail *pieceTrail
	cfg := llmkit.SessionConfig{AppendSystem: "x"}
	trail.mark(&cfg, domain.PieceOther, "X", "Y") // must not panic
}

func TestPieceTrail_TwoPiecesWithOneTitleGetDistinctIDs(t *testing.T) {
	cfg := llmkit.SessionConfig{}
	trail := &pieceTrail{}
	cfg.AppendSystem = appendSection(cfg.AppendSystem, "a")
	trail.mark(&cfg, domain.PieceOther, "Note", "H")
	cfg.AppendSystem = appendSection(cfg.AppendSystem, "b")
	trail.mark(&cfg, domain.PieceOther, "Note", "H")
	if trail.pieces[0].ID == trail.pieces[1].ID {
		t.Errorf("ids = %q %q", trail.pieces[0].ID, trail.pieces[1].ID)
	}
}

func contextCfg() llmkit.SessionConfig {
	return llmkit.SessionConfig{
		InstructionFiles: []llmkit.InstructionFile{
			{Kind: llmkit.InstructionMemory, Path: "ARCHITECTURE.md", Content: []byte("arch summary")},
			{Kind: llmkit.InstructionMemory, Path: "CLAUDE.md", Content: []byte("root memory")},
			{Kind: llmkit.InstructionSkill, Path: ".claude/skills/x/SKILL.md", Content: []byte("a skill")},
			{Kind: llmkit.InstructionMemory, Path: "BRANCH_GATE.md", Content: []byte("gate rules")},
		},
	}
}

func TestBuildContextEntry_PiecesComeInSendOrderThenFilesThenTheOpeningMessage(t *testing.T) {
	system := []domain.ContextPiece{
		domain.NewContextPiece("repository-prompt", domain.PieceRepositoryPrompt, "Repository prompt", "Repository api", "", "", "repo prompt"),
		domain.NewContextPiece("task-brief", domain.PieceBrief, "Task brief", "Harness", "", "", "brief"),
	}
	e := buildContextEntry(contextInput{
		Kind: domain.ContextEntryStart, At: time.Unix(5, 0), ID: "e1",
		System: system, Cfg: contextCfg(), Versions: map[string]string{"CLAUDE.md": "3"},
		Repository: "api", Opening: "start the work",
	})
	var order []string
	for _, p := range e.Instructions {
		order = append(order, p.ID)
	}
	want := []string{"repository-prompt", "task-brief", "ARCHITECTURE.md", "CLAUDE.md", "BRANCH_GATE.md", "opening-message"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	byID := map[string]domain.ContextPiece{}
	for _, p := range e.Instructions {
		byID[p.ID] = p
	}
	if p := byID["CLAUDE.md"]; p.Kind != domain.PieceInstructionFile || p.Path != "CLAUDE.md" || p.Version != "3" ||
		p.Source != "Instruction files · api" || p.Text != "root memory" || p.Bytes != 11 {
		t.Errorf("CLAUDE.md = %+v", p)
	}
	if p := byID["ARCHITECTURE.md"]; p.Kind != domain.PieceArchitecture || p.Version != "" {
		t.Errorf("ARCHITECTURE.md = %+v", p)
	}
	if p := byID["BRANCH_GATE.md"]; p.Kind != domain.PieceBranchGate {
		t.Errorf("BRANCH_GATE.md = %+v", p)
	}
	if p := byID["opening-message"]; p.Kind != domain.PieceOpeningMessage || p.Text != "start the work" {
		t.Errorf("opening = %+v", p)
	}
	if _, ok := byID[".claude/skills/x/SKILL.md"]; ok {
		t.Error("a skill file is a skill, not an instruction piece")
	}
	sum := 0
	for _, p := range e.Instructions {
		sum += p.ApproxTokens
	}
	if e.Facts.ApproxTokens != sum || sum == 0 {
		t.Errorf("approx tokens = %d, pieces sum to %d", e.Facts.ApproxTokens, sum)
	}
	if e.ID != "e1" || e.Kind != domain.ContextEntryStart || !e.At.Equal(time.Unix(5, 0)) {
		t.Errorf("entry = %+v", e)
	}
}

func TestBuildContextEntry_NoOpeningMessageMakesNoPiece(t *testing.T) {
	e := buildContextEntry(contextInput{Kind: domain.ContextEntryStart, Cfg: llmkit.SessionConfig{}})
	if len(e.Instructions) != 0 {
		t.Errorf("pieces = %+v", e.Instructions)
	}
}

func TestBuildContextEntry_NoSecretIsStored(t *testing.T) {
	cfg := contextCfg()
	cfg.Env = []string{"ANTHROPIC_API_KEY=sk-secret-env", "PATH=/bin"}
	cfg.MCPServers = []llmkit.MCPServerSpec{{
		Name: "github", Transport: "http", URL: "https://x/mcp", Command: "run", Args: []string{"--token", "arg-secret"},
		Env:     map[string]string{"GITHUB_TOKEN": "env-secret"},
		Headers: map[string]string{"Authorization": "Bearer header-secret"},
	}}
	e := buildContextEntry(contextInput{Kind: domain.ContextEntryStart, ID: "e", Cfg: cfg, Repository: "api"})
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"sk-secret-env", "ANTHROPIC_API_KEY", "env-secret", "GITHUB_TOKEN", "header-secret", "Authorization", "arg-secret"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("entry leaks %q: %s", leak, b)
		}
	}
}
