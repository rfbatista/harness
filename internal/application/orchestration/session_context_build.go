package orchestration

import (
	"strconv"
	"strings"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// pieceTrail follows a session's system prompt as it is built, section by
// section, so the context entry can say which piece is which (ADR 0099). The
// sections are joined into one string as they are appended, so each mark
// takes what was added since the last one. A nil trail marks nothing.
type pieceTrail struct {
	seen   string
	pieces []domain.ContextPiece
}

// mark closes the text appended to cfg's system prompt since the last mark as
// one piece. Nothing appended makes no piece.
func (t *pieceTrail) mark(cfg *llmkit.SessionConfig, kind, title, source string) {
	if t == nil {
		return
	}
	now := cfg.AppendSystem
	// appendSection trims the trailing newlines of what it extends.
	text := now
	if base := strings.TrimRight(t.seen, "\n"); base != "" && strings.HasPrefix(now, base) {
		text = strings.TrimLeft(strings.TrimPrefix(now, base), "\n")
	} else if t.seen != "" && now != t.seen {
		// Rewritten rather than extended: nothing here can say what changed.
		t.seen = now
		return
	}
	t.seen = now
	if text == "" {
		return
	}
	t.pieces = append(t.pieces, domain.NewContextPiece(t.id(title), kind, title, source, "", "", text))
}

// id is a piece's id from its title, made unique among the trail's pieces.
func (t *pieceTrail) id(title string) string {
	base := domain.Slug(title)
	id := base
	for n := 2; t.has(id); n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

func (t *pieceTrail) has(id string) bool {
	for _, p := range t.pieces {
		if p.ID == id {
			return true
		}
	}
	return false
}

// contextInput is what an entry is built from: the pieces the system prompt
// trail found and the resolved configuration, at the moment it is handed to
// claude. The builder reads named fields of the configuration only: never its
// environment, an MCP server's environment, arguments or headers (contract C2).
type contextInput struct {
	ID    string
	Kind  string
	At    time.Time
	Facts domain.ContextFacts

	System     []domain.ContextPiece
	Cfg        llmkit.SessionConfig
	Versions   map[string]string // instruction file path -> version
	Repository string            // the repository's name, for the pieces' source
	Opening    string

	// What the tools, servers, skills and hooks are worked out from.
	Role       domain.SessionRole
	Agent      *domain.Agent
	Project    *domain.Project
	Attached   mcpAttachment         // the catalog servers the session got, and the tools it hides
	KnownTools map[string][]string   // each catalog server's tool names, by server name
	Skills     []domain.ContextSkill // the mode's and the agent's skills
	BuiltIn    []string              // Claude's built-in tools: what a session with no allow-list can use
}

// buildContextEntry builds the entry for one start or resume. Pieces go in
// send order: the system prompt's pieces, the instruction files delivered as
// memory, then the opening message.
func buildContextEntry(in contextInput) domain.ContextEntry {
	e := domain.ContextEntry{ID: in.ID, Kind: in.Kind, At: in.At, Facts: in.Facts}
	e.Instructions = append(e.Instructions, in.System...)
	for _, f := range in.Cfg.InstructionFiles {
		if f.Kind != llmkit.InstructionMemory {
			continue
		}
		kind, title := domain.PieceInstructionFile, f.Path
		switch f.Path {
		case "ARCHITECTURE.md":
			kind, title = domain.PieceArchitecture, "Architecture summary"
		case "BRANCH_GATE.md":
			kind, title = domain.PieceBranchGate, "Branch rules"
		}
		source := "Instruction files"
		if in.Repository != "" {
			source += " · " + in.Repository
		}
		p := domain.NewContextPiece(f.Path, kind, title, source, f.Path, in.Versions[f.Path], string(f.Content))
		e.Instructions = append(e.Instructions, p)
	}
	if in.Opening != "" {
		e.Instructions = append(e.Instructions,
			domain.NewContextPiece("opening-message", domain.PieceOpeningMessage, "Opening message", "Harness", "", "", in.Opening))
	}
	e.Tools, e.MCPServers = contextToolsAndServers(in)
	e.Skills = contextSkills(in)
	e.Hooks = contextHooks(in.Cfg)
	e.Facts.ApproxTokens = 0
	for _, p := range e.Instructions {
		e.Facts.ApproxTokens += p.ApproxTokens
	}
	return e
}

// briefTitle is what the session's brief is called in its context.
func (s sessionSubject) briefTitle() string {
	switch {
	case s.designSystem != nil:
		return "Design session brief"
	case s.mode == domain.SessionModeProductOwner && s.project != nil:
		return "Product owner brief"
	case s.projectLevel():
		return "Project brief"
	}
	return "Task brief"
}
