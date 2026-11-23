package domain

import (
	"time"
	"unicode/utf8"
)

// ContextPieceTextLimit is how many bytes of a piece's text a read of the
// session context carries (ADR 0099); the rest is read on demand.
const ContextPieceTextLimit = 4096

// Context entry kinds.
const (
	ContextEntryStart  = "start"
	ContextEntryResume = "resume"
)

// Context change kinds (contract "Harness server ↔ Web UI — session context").
const (
	ContextChangeEntryAdded    = "entry_added"
	ContextChangeSkillInvoked  = "skill_invoked"
	ContextChangeToolFirstUsed = "tool_first_used"
	ContextChangeMCPConnected  = "mcp_connected"
	ContextChangeMCPFailed     = "mcp_failed"
)

// Context piece kinds.
const (
	PieceRepositoryPrompt = "repository_prompt"
	PieceBrief            = "brief"
	PieceOpeningMessage   = "opening_message"
	PieceArchitecture     = "architecture"
	PieceBranchGate       = "branch_gate"
	PieceInstructionFile  = "instruction_file"
	PieceRuleset          = "ruleset"
	PieceMemory           = "memory"
	PieceMCPInstructions  = "mcp_instructions"
	PieceOther            = "other"
)

// Tool states.
const (
	ToolAllowed  = "allowed"
	ToolOnDemand = "on_demand"
	ToolHidden   = "hidden"
	ToolRefused  = "refused"
)

// How an MCP server was attached to a session.
const (
	AttachedBuiltIn    = "built_in"
	AttachedTokenSaver = "token_saver"
	AttachedAgent      = "agent"
	AttachedProject    = "project"
	AttachedMode       = "mode"
)

// MCP server statuses. Only claude reports connected or failed; the harness
// never probes a server (ADR 0099).
const (
	MCPConnected = "connected"
	MCPFailed    = "failed"
	MCPUnknown   = "unknown"
)

// MCPStatusUnknownReason is why a terminal session's servers have no status.
const MCPStatusUnknownReason = "claude does not report it in a terminal session"

// ContextFacts are the session's setup at a glance.
type ContextFacts struct {
	Agent                string   `json:"agent"`
	Role                 string   `json:"role"`
	Mode                 string   `json:"mode"`
	Model                string   `json:"model"`
	Headless             bool     `json:"headless"`
	PermissionMode       string   `json:"permission_mode"`
	Sandbox              string   `json:"sandbox"`
	RepositoryVisibility string   `json:"repository_visibility"`
	Worktree             string   `json:"worktree"`
	Branch               string   `json:"branch"`
	TokenSavers          []string `json:"token_savers"`
	ClaudeVersion        string   `json:"claude_version"`
	ApproxTokens         int      `json:"approx_tokens"`
}

// ContextPieceChange marks a piece that differs from the previous entry's.
type ContextPieceChange struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
}

// ContextPiece is one part of the instructions a session was handed. Its text
// is stored whole; a read cuts it (see ContextEntry.View).
type ContextPiece struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	Title        string              `json:"title"`
	Source       string              `json:"source"`
	Path         string              `json:"path"`
	Version      string              `json:"version"`
	Bytes        int                 `json:"bytes"`
	ApproxTokens int                 `json:"approx_tokens"`
	Text         string              `json:"text"`
	Truncated    bool                `json:"truncated"`
	Changed      *ContextPieceChange `json:"changed"`
}

// NewContextPiece builds a piece with its size derived from text.
func NewContextPiece(id, kind, title, source, path, version, text string) ContextPiece {
	return ContextPiece{ID: id, Kind: kind, Title: title, Source: source, Path: path, Version: version,
		Bytes: len(text), ApproxTokens: ApproxTokens(len(text)), Text: text}
}

// ApproxTokens is the rough token count of n bytes: n/4, rounded.
func ApproxTokens(n int) int { return (n + 2) / 4 }

// CutText cuts s to at most limit bytes on a UTF-8 boundary.
func CutText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// ContextTool is a tool the session can use, with its state.
type ContextTool struct {
	Name   string `json:"name"`
	Source string `json:"source"` // claude_code | mcp
	Server string `json:"server"`
	State  string `json:"state"`
}

// ContextServer is an MCP server the session got.
type ContextServer struct {
	Name           string   `json:"name"`
	Attached       string   `json:"attached"` // built_in | token_saver | agent | project | mode
	AttachedReason string   `json:"attached_reason"`
	Status         string   `json:"status"`
	StatusReason   string   `json:"status_reason"`
	HiddenTools    []string `json:"hidden_tools"`
}

// ContextSkill is a skill the session has.
type ContextSkill struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // mode | repository | agent | built_in
	Description string `json:"description"`
}

// ContextHook is a hook rule the session got.
type ContextHook struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher"`
	Rule    string `json:"rule"`
}

// ContextEntry is the context the harness handed to claude at one start or
// resume. It is never recomputed from current settings.
type ContextEntry struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"` // start | resume
	At           time.Time       `json:"at"`
	Facts        ContextFacts    `json:"facts"`
	Instructions []ContextPiece  `json:"instructions"`
	Tools        []ContextTool   `json:"tools"`
	MCPServers   []ContextServer `json:"mcp_servers"`
	Skills       []ContextSkill  `json:"skills"`
	Hooks        []ContextHook   `json:"hooks"`
}

// View is the entry as a read serves it: piece texts cut at
// ContextPieceTextLimit, and every list non-nil.
func (e ContextEntry) View() ContextEntry {
	out := e
	out.Instructions = make([]ContextPiece, len(e.Instructions))
	for i, p := range e.Instructions {
		p.Text = CutText(p.Text, ContextPieceTextLimit)
		p.Truncated = len(p.Text) < p.Bytes
		out.Instructions[i] = p
	}
	out.Tools = orEmpty(e.Tools)
	out.MCPServers = orEmpty(e.MCPServers)
	out.Skills = orEmpty(e.Skills)
	out.Hooks = orEmpty(e.Hooks)
	out.Facts.TokenSavers = orEmpty(e.Facts.TokenSavers)
	return out
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Piece finds a piece of the entry by id.
func (e ContextEntry) Piece(id string) (ContextPiece, bool) {
	for _, p := range e.Instructions {
		if p.ID == id {
			return p, true
		}
	}
	return ContextPiece{}, false
}

// ContextUse is how often a tool or skill was used, and when last.
type ContextUse struct {
	Count  int       `json:"count"`
	LastAt time.Time `json:"last_at"`
}

// ContextUsage is the session's use of tools and skills, from its transcript.
type ContextUsage struct {
	Tools  map[string]ContextUse `json:"tools"`
	Skills map[string]ContextUse `json:"skills"`
}

// ContextChange is one thing that happened to the context while it ran.
type ContextChange struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Subject string    `json:"subject"`
	Detail  string    `json:"detail"`
}

// SessionContext is everything the harness stores about a session's context:
// its entries oldest first, its usage, and its changes oldest first. Cursor is
// how much of the transcript the usage has read; it is never served.
type SessionContext struct {
	SessionID string
	Entries   []ContextEntry
	Usage     ContextUsage
	Changes   []ContextChange
	Cursor    string
}

// Entry finds an entry by id.
func (c *SessionContext) Entry(id string) (ContextEntry, bool) {
	for _, e := range c.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return ContextEntry{}, false
}

// SessionContextView is the answer to a read of a session's context.
type SessionContextView struct {
	SessionID string          `json:"session_id"`
	Recorded  bool            `json:"recorded"`
	Entries   []ContextEntry  `json:"entries"`
	Usage     ContextUsage    `json:"usage"`
	Changes   []ContextChange `json:"changes"` // newest first
}

// View is the stored context as a read serves it; nil is a session that
// started before the harness recorded context.
func (c *SessionContext) View(sessionID string) SessionContextView {
	v := SessionContextView{SessionID: sessionID, Entries: []ContextEntry{}, Changes: []ContextChange{},
		Usage: ContextUsage{Tools: map[string]ContextUse{}, Skills: map[string]ContextUse{}}}
	if c == nil || len(c.Entries) == 0 {
		return v
	}
	v.Recorded = true
	for _, e := range c.Entries {
		v.Entries = append(v.Entries, e.View())
	}
	for tool, u := range c.Usage.Tools {
		v.Usage.Tools[tool] = u
	}
	for skill, u := range c.Usage.Skills {
		v.Usage.Skills[skill] = u
	}
	for i := len(c.Changes) - 1; i >= 0; i-- {
		v.Changes = append(v.Changes, c.Changes[i])
	}
	return v
}
