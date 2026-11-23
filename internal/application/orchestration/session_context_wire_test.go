package orchestration

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pieceCut stores one piece of text, reads it back through the service and
// returns what the read served and what the piece endpoint served.
func pieceCut(t *testing.T, text string) (served domain.ContextPiece, whole string) {
	t.Helper()
	svc := contextService(t)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(1, 0),
		Instructions: []domain.ContextPiece{domain.NewContextPiece("p", domain.PieceOther, "P", "H", "", "", text)}})
	p, _ := contextOf(t, svc, "s1").Entries[0].Piece("p")
	whole, err := svc.SessionContextPiece(context.Background(), "s1", "e1", "p")
	if err != nil {
		t.Fatal(err)
	}
	return p, whole
}

func TestSessionContextCut_EveryRuneWidthAtEveryAlignmentCutsOnABoundary(t *testing.T) {
	runes := map[string]string{"1 byte": "a", "2 bytes": "é", "3 bytes": "€", "4 bytes": "😀"}
	for name, r := range runes {
		for offset := 0; offset < 4; offset++ { // shifts where the 4096 mark falls inside a rune
			text := strings.Repeat("x", offset) + strings.Repeat(r, 5000/len(r))
			p, whole := pieceCut(t, text)
			if !utf8.ValidString(p.Text) {
				t.Errorf("%s +%d: the cut is not valid UTF-8", name, offset)
			}
			if len(p.Text) > domain.ContextPieceTextLimit || len(p.Text) < domain.ContextPieceTextLimit-3 {
				t.Errorf("%s +%d: cut is %d bytes", name, offset, len(p.Text))
			}
			if !strings.HasPrefix(text, p.Text) {
				t.Errorf("%s +%d: the cut is not the start of the text", name, offset)
			}
			if !p.Truncated || p.Bytes != len(text) || p.ApproxTokens != (len(text)+2)/4 {
				t.Errorf("%s +%d: truncated=%v bytes=%d tokens=%d", name, offset, p.Truncated, p.Bytes, p.ApproxTokens)
			}
			if whole != text {
				t.Errorf("%s +%d: the piece endpoint did not answer the whole text", name, offset)
			}
		}
	}
}

func TestSessionContextCut_TheLimitIsTheOnlyThingThatCuts(t *testing.T) {
	exact := strings.Repeat("a", domain.ContextPieceTextLimit)
	if p, _ := pieceCut(t, exact); p.Truncated || p.Text != exact || p.Bytes != 4096 {
		t.Errorf("a piece of exactly 4096 bytes: truncated=%v len=%d", p.Truncated, len(p.Text))
	}
	if p, _ := pieceCut(t, exact+"b"); !p.Truncated || len(p.Text) != 4096 || p.Bytes != 4097 {
		t.Errorf("a piece of 4097 bytes: truncated=%v len=%d bytes=%d", p.Truncated, len(p.Text), p.Bytes)
	}
	// 4095 ASCII bytes then a 2-byte rune ends at byte 4097: the rune is cut off whole.
	straddle := strings.Repeat("a", 4095) + "é"
	p, _ := pieceCut(t, straddle)
	if p.Text != strings.Repeat("a", 4095) || !p.Truncated {
		t.Errorf("a rune straddling the limit: len=%d truncated=%v", len(p.Text), p.Truncated)
	}
	// 4094 + a 2-byte rune ends exactly on 4096: it stays, and nothing is cut.
	fits := strings.Repeat("a", 4094) + "é"
	if p, _ := pieceCut(t, fits); p.Truncated || p.Text != fits {
		t.Errorf("a rune ending on the limit: len=%d truncated=%v", len(p.Text), p.Truncated)
	}
	if p, _ := pieceCut(t, ""); p.Truncated || p.Text != "" || p.Bytes != 0 {
		t.Errorf("an empty piece = %+v", p)
	}
}

func TestSessionContextCut_AServedPieceMarshalsAsValidJSONWhateverTheCut(t *testing.T) {
	p, _ := pieceCut(t, strings.Repeat("日本語", 2000))
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.ContextPiece
	if err := json.Unmarshal(b, &back); err != nil || back.Text != p.Text {
		t.Errorf("the cut does not survive the wire: %v", err)
	}
	if strings.ContainsRune(back.Text, utf8.RuneError) {
		t.Error("a split rune became U+FFFD on the wire")
	}
}

func TestSessionContextCut_ReadingNeverShortensTheStoredPiece(t *testing.T) {
	svc := contextService(t)
	long := strings.Repeat("é", 4000)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(1, 0),
		Instructions: []domain.ContextPiece{domain.NewContextPiece("p", domain.PieceOther, "P", "H", "", "", long)}})
	contextOf(t, svc, "s1")
	contextOf(t, svc, "s1")
	stored, _ := svc.Contexts.Load(context.Background(), "s1")
	if got := stored.Entries[0].Instructions[0]; got.Text != long || got.Truncated {
		t.Errorf("a read cut what is stored: %d bytes, truncated=%v", len(got.Text), got.Truncated)
	}
}

func keysOf(t *testing.T, b []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sub follows path through objects; a list on the way, or at the end, is
// replaced by its first element.
func sub(t *testing.T, b []byte, path ...string) []byte {
	t.Helper()
	unwrap := func(b []byte) []byte {
		if len(b) > 0 && b[0] == '[' {
			var list []json.RawMessage
			if err := json.Unmarshal(b, &list); err != nil || len(list) == 0 {
				t.Fatalf("an empty list: %s", b)
			}
			return list[0]
		}
		return b
	}
	for _, p := range path {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(unwrap(b), &m); err != nil {
			t.Fatal(err)
		}
		raw, ok := m[p]
		if !ok {
			t.Fatalf("no %q in %s", p, b)
		}
		b = raw
	}
	return unwrap(b)
}

func sameKeys(t *testing.T, what string, b []byte, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := keysOf(t, b); !slices.Equal(got, want) {
		t.Errorf("%s keys = %v, want %v", what, got, want)
	}
}

// TestSessionContextWire_TheViewUsesTheContractsKeysAndNoOthers marshals a read
// that holds one of everything and checks every object's keys (rule C6): a
// renamed or extra field breaks the page.
func TestSessionContextWire_TheViewUsesTheContractsKeysAndNoOthers(t *testing.T) {
	svc, tr := usageService(t)
	tr.add(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC), ports.TranscriptToolUse, "Skill", "task-tdd")
	storeEntry(t, svc, entryWith("e2", domain.ContextEntryResume, time.Unix(20, 0), fileP("CLAUDE.md", "1", "a")))
	storeEntry(t, svc, entryWith("e3", domain.ContextEntryResume, time.Unix(30, 0), fileP("CLAUDE.md", "2", "b")))
	e := entryWith("e4", domain.ContextEntryResume, time.Unix(40, 0), fileP("CLAUDE.md", "3", "c"))
	e.Tools = []domain.ContextTool{{Name: "Bash", Source: "claude_code", State: domain.ToolAllowed}}
	e.MCPServers = []domain.ContextServer{{Name: "task", Attached: domain.AttachedBuiltIn, Status: domain.MCPUnknown}}
	e.Skills = []domain.ContextSkill{{Name: "s", Source: "mode"}}
	e.Hooks = []domain.ContextHook{{Event: "PreToolUse", Matcher: "Bash", Rule: "r"}}
	storeEntry(t, svc, e)
	syncUsage(t, svc)

	b, err := json.Marshal(contextOf(t, svc, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	sameKeys(t, "view", b, "session_id", "recorded", "entries", "usage", "changes")
	var raw struct {
		Entries []json.RawMessage `json:"entries"`
	}
	_ = json.Unmarshal(b, &raw)
	last := raw.Entries[len(raw.Entries)-1]
	sameKeys(t, "entry", last, "id", "kind", "at", "facts", "instructions", "tools", "mcp_servers", "skills", "hooks")
	sameKeys(t, "facts", sub(t, last, "facts"), "agent", "role", "mode", "model", "headless", "permission_mode", "sandbox",
		"repository_visibility", "worktree", "branch", "token_savers", "claude_version", "approx_tokens")
	sameKeys(t, "piece", sub(t, last, "instructions"), "id", "kind", "title", "source", "path", "version", "bytes", "approx_tokens", "text", "truncated", "changed")
	sameKeys(t, "changed", sub(t, last, "instructions", "changed"), "from_version", "to_version")
	sameKeys(t, "tool", sub(t, last, "tools"), "name", "source", "server", "state")
	sameKeys(t, "server", sub(t, last, "mcp_servers"), "name", "attached", "attached_reason", "status", "status_reason", "hidden_tools")
	sameKeys(t, "skill", sub(t, last, "skills"), "name", "source", "description")
	sameKeys(t, "hook", sub(t, last, "hooks"), "event", "matcher", "rule")
	sameKeys(t, "usage", sub(t, b, "usage"), "tools", "skills")
	sameKeys(t, "change", sub(t, b, "changes"), "at", "kind", "subject", "detail")
	var u struct {
		Usage struct {
			Skills map[string]map[string]any `json:"skills"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(b, &u)
	use := u.Usage.Skills["task-tdd"]
	if len(use) != 2 || use["count"] == nil || use["last_at"] == nil {
		t.Errorf("a use = %v, want exactly count and last_at", use)
	}
	if _, err := time.Parse(time.RFC3339, use["last_at"].(string)); err != nil {
		t.Errorf("last_at is not RFC 3339: %v", err)
	}
}

func TestSessionContextWire_TheCursorIsNeverServed(t *testing.T) {
	svc, tr := usageService(t)
	tr.add(time.Now(), ports.TranscriptToolUse, "Bash", "x")
	syncUsage(t, svc)
	b, _ := json.Marshal(contextOf(t, svc, "s1"))
	if strings.Contains(strings.ToLower(string(b)), "cursor") {
		t.Errorf("the served context carries the usage cursor: %s", b)
	}
}

func TestSessionContextWire_ANotRecordedViewIsEmptyListsAndMapsNotNull(t *testing.T) {
	svc := contextService(t)
	b, _ := json.Marshal(contextOf(t, svc, "s1"))
	if string(b) != `{"session_id":"s1","recorded":false,"entries":[],"usage":{"tools":{},"skills":{}},"changes":[]}` {
		t.Errorf("not-recorded view = %s", b)
	}
}

func TestSessionContextWire_EmptyListsOfAnEntryAreEmptyNotNull(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(1, 0)})
	b, _ := json.Marshal(contextOf(t, svc, "s1"))
	for _, null := range []string{`"instructions":null`, `"tools":null`, `"mcp_servers":null`, `"skills":null`, `"hooks":null`, `"token_savers":null`, `"changes":null`, `"entries":null`} {
		if strings.Contains(string(b), null) {
			t.Errorf("view has %s", null)
		}
	}
}

func TestSessionContextWire_ChangesComeNewestFirstEvenWhenFoundOutOfOrder(t *testing.T) {
	svc, tr := usageService(t)
	t0 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	// The transcript is read after the fact: this skill ran an hour after the
	// start entry was stored, and an earlier one is found second.
	tr.add(t0.Add(time.Hour), ports.TranscriptToolUse, "Skill", "late")
	tr.add(t0.Add(30*time.Minute), ports.TranscriptToolUse, "Skill", "early")
	syncUsage(t, svc)
	ch := contextOf(t, svc, "s1").Changes
	for i := 1; i < len(ch); i++ {
		if ch[i].At.After(ch[i-1].At) {
			t.Errorf("changes not newest first at %d: %v then %v", i, ch[i-1].At, ch[i].At)
		}
	}
}
