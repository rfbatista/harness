package claudehome

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/ports"
)

// The fixture records below copy the shapes claude 2.1.296 writes, trimmed
// to the fields that matter plus a few that do not.

const ts = "2026-10-10T17:39:30.870Z"

func userText(s string) string {
	return rec(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": s}})
}

func rec(m map[string]any) string {
	if _, ok := m["timestamp"]; !ok {
		m["timestamp"] = ts
	}
	m["uuid"] = "u"
	m["sessionId"] = "c1"
	m["cwd"] = "/w"
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func assistant(parts ...map[string]any) string {
	return rec(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": parts}})
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResult(id string, content any, isError bool) string {
	return rec(map[string]any{
		"type":          "user",
		"message":       map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isError}}},
		"toolUseResult": map[string]any{"stdout": strings.Repeat("x", 100)},
	})
}

type want struct{ kind, text, tool string }

func TestMapLine(t *testing.T) {
	tools := map[string]string{"toolu_1": "Bash"}
	img := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": "iVBORw0KGgo="}}
	cases := []struct {
		name string
		line string
		want []want
	}{
		{"typed prompt", userText("fix the bug"), []want{{"user", "fix the bug", ""}}},
		{"slash command", userText("<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>opus</command-args>"), []want{{"user", "/model opus", ""}}},
		{"slash command without args", userText("<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"), []want{{"user", "/clear", ""}}},
		{"command output", userText("<local-command-stdout>\x1b[2mCompacted\x1b[22m</local-command-stdout>"), []want{{"system", "Compacted", ""}}},
		{"empty command output", userText("<local-command-stdout></local-command-stdout>"), nil},
		{"command caveat", userText("<local-command-caveat>Caveat: …</local-command-caveat>"), nil},
		{"task notification", userText("<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n<summary>Background command \"make check\" completed (exit code 0)</summary>\n</task-notification>"), []want{{"system", `Background command "make check" completed (exit code 0)`, ""}}},
		{"interrupted", userText("[Request interrupted by user for tool use]"), []want{{"system", "[Request interrupted by user for tool use]", ""}}},
		{"stop hook feedback", rec(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": "Stop hook feedback:\n[task message 1 · reply]"}}), []want{{"system", "Stop hook feedback:\n[task message 1 · reply]", ""}}},
		{"other meta", rec(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": "[Image: original 1393x5547]"}}), nil},
		{"meta skill body", rec(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Base directory for this skill"}}}}), nil},
		{"compact summary", rec(map[string]any{"type": "user", "isCompactSummary": true, "message": map[string]any{"content": "This session is being continued…"}}), nil},
		{"text and image parts", rec(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "look"}, img}}}), []want{{"user", "look", ""}, {"user", "[image]", ""}}},
		{"tool result string", toolResult("toolu_1", "ok\n", false), []want{{"tool_result", "ok\n", "Bash"}}},
		{"tool result parts", toolResult("toolu_1", []any{map[string]any{"type": "text", "text": "a"}, img, map[string]any{"type": "tool_reference", "tool_name": "X"}}, false), []want{{"tool_result", "a\n[image]", "Bash"}}},
		{"tool result of an unknown call", toolResult("toolu_9", "", true), []want{{"tool_result", "", ""}}},
		{"assistant text", assistant(map[string]any{"type": "text", "text": "Done. **All** green."}), []want{{"assistant", "Done. **All** green.", ""}}},
		{"blank assistant text", assistant(map[string]any{"type": "text", "text": "\n\n"}), nil},
		{"thinking", assistant(map[string]any{"type": "thinking", "thinking": "hmm", "signature": "s"}), nil},
		{"tool use", assistant(toolUse("toolu_2", "Bash", map[string]any{"command": "go test ./...\n  -run X", "description": "Run tests"})), []want{{"tool_use", "go test ./... -run X", "Bash"}}},
		{"tool use pattern", assistant(toolUse("toolu_3", "Grep", map[string]any{"pattern": "TODO", "path": "internal"})), []want{{"tool_use", "TODO in internal", "Grep"}}},
		{"tool use other input", assistant(toolUse("toolu_4", "mcp__task__get_task", map[string]any{"a": 1})), []want{{"tool_use", `{"a":1}`, "mcp__task__get_task"}}},
		{"tool use no input", assistant(toolUse("toolu_5", "mcp__task__get_task", map[string]any{})), []want{{"tool_use", "", "mcp__task__get_task"}}},
		{"api error", rec(map[string]any{"type": "assistant", "isApiErrorMessage": true, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "API Error: 529 overloaded"}}}}), []want{{"system", "API Error: 529 overloaded", ""}}},
		{"compact boundary", rec(map[string]any{"type": "system", "subtype": "compact_boundary", "content": "Conversation compacted", "level": "info"}), []want{{"system", "Conversation compacted", ""}}},
		{"informational", rec(map[string]any{"type": "system", "subtype": "informational", "content": "Compacted while idle", "level": "notice"}), []want{{"system", "Compacted while idle", ""}}},
		{"local command", rec(map[string]any{"type": "system", "subtype": "local_command", "content": "<command-name>/skills</command-name>\n<command-message>skills</command-message>\n<command-args></command-args>"}), []want{{"system", "/skills", ""}}},
		{"system error level", rec(map[string]any{"type": "system", "subtype": "new_thing", "content": "boom", "level": "error"}), []want{{"system", "boom", ""}}},
		{"turn duration", rec(map[string]any{"type": "system", "subtype": "turn_duration", "durationMs": 10}), nil},
		{"stop hook summary", rec(map[string]any{"type": "system", "subtype": "stop_hook_summary", "level": "suggestion"}), nil},
		{"queued prompt", rec(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "prompt": "also run lint", "commandMode": "prompt", "origin": map[string]any{"kind": "human"}}}), []want{{"user", "also run lint", ""}}},
		{"queued notification", rec(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "prompt": "<task-notification><summary>done</summary></task-notification>", "commandMode": "task-notification"}}), []want{{"system", "done", ""}}},
		{"other attachment", rec(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "total_tokens_reminder", "content": "…"}}), nil},
		{"sidechain", rec(map[string]any{"type": "user", "isSidechain": true, "message": map[string]any{"content": "sub agent"}}), nil},
		{"unknown type", rec(map[string]any{"type": "ai-title", "aiTitle": "x"}), nil},
		{"not json", `{"type": "user", "message": `, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapLine([]byte(c.line), func(id string) string { return tools[id] })
			if len(got) != len(c.want) {
				t.Fatalf("got %d entries %+v, want %d", len(got), got, len(c.want))
			}
			for i, w := range c.want {
				g := got[i]
				if g.Kind != w.kind || g.Text != w.text || g.Tool != w.tool {
					t.Errorf("entry %d = {%s %q %q}, want {%s %q %q}", i, g.Kind, g.Text, g.Tool, w.kind, w.text, w.tool)
				}
				if g.At != ts {
					t.Errorf("entry %d at = %q, want %q", i, g.At, ts)
				}
			}
		})
	}
}

func TestMapLineFlagsErrorsTruncationAndBadTimestamps(t *testing.T) {
	got := mapLine([]byte(toolResult("toolu_1", "failed", true)), nil)
	if len(got) != 1 || !got[0].IsError {
		t.Errorf("failed tool result = %+v, want is_error", got)
	}
	long := strings.Repeat("é", maxEntryText) // 2 bytes a rune
	got = mapLine([]byte(userText(long)), nil)
	if len(got) != 1 || !got[0].Truncated || len(got[0].Text) > maxEntryText || !strings.HasPrefix(long, got[0].Text) {
		t.Errorf("long text: truncated=%v len=%d", got[0].Truncated, len(got[0].Text))
	}
	got = mapLine([]byte(rec(map[string]any{"type": "user", "timestamp": "yesterday", "message": map[string]any{"content": "hi"}})), nil)
	if len(got) != 1 || got[0].At != "" {
		t.Errorf("bad timestamp: %+v, want at \"\"", got)
	}
	got = mapLine([]byte(assistant(toolUse("t", "Bash", map[string]any{"command": strings.Repeat("a", 500)}))), nil)
	if n := len([]rune(got[0].Text)); n != maxSummary || strings.Contains(got[0].Text, "\n") {
		t.Errorf("summary has %d runes, want %d on one line", n, maxSummary)
	}
}

// home is a CLI config directory with a worktree whose conversation c1 holds
// lines.
type home struct {
	dir, work, file string
	r               *TranscriptReader
}

func newHome(t testing.TB, lines ...string) *home {
	t.Helper()
	h := &home{dir: t.TempDir(), work: filepath.Join(t.TempDir(), "agent-x")}
	if err := os.MkdirAll(h.work, 0o755); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(h.work)
	proj := filepath.Join(h.dir, "projects", nonAlnum.ReplaceAllString(real, "-"))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	h.file = filepath.Join(proj, "c1.jsonl")
	h.write(t, lines...)
	h.r = NewTranscriptReader(h.dir)
	return h
}

func (h *home) write(t testing.TB, lines ...string) {
	t.Helper()
	if err := os.WriteFile(h.file, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *home) append(t *testing.T, s string) {
	t.Helper()
	f, err := os.OpenFile(h.file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func (h *home) read(t *testing.T, q ports.TranscriptQuery) ports.TranscriptPage {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = ports.TranscriptDefaultLimit
	}
	p, err := h.r.ReadTranscript(h.work, "c1", q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func texts(p ports.TranscriptPage) []string {
	var out []string
	for _, e := range p.Entries {
		out = append(out, e.Text)
	}
	return out
}

func numbered(n int) []string {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines, userText(fmt.Sprint(i)))
	}
	return lines
}

func TestReadTranscriptPages(t *testing.T) {
	h := newHome(t, numbered(10)...)

	p := h.read(t, ports.TranscriptQuery{Limit: 4})
	if got := strings.Join(texts(p), ","); got != "7,8,9,10" || !p.HasOlder || p.HasNewer {
		t.Fatalf("newest page = %s older=%v newer=%v", got, p.HasOlder, p.HasNewer)
	}
	if p.FirstCursor != p.Entries[0].Cursor || p.LastCursor != p.Entries[3].Cursor {
		t.Errorf("first/last cursor %q %q do not match the entries", p.FirstCursor, p.LastCursor)
	}

	p = h.read(t, ports.TranscriptQuery{Before: p.FirstCursor, Limit: 4})
	if got := strings.Join(texts(p), ","); got != "3,4,5,6" || !p.HasOlder || !p.HasNewer {
		t.Fatalf("before page = %s older=%v newer=%v", got, p.HasOlder, p.HasNewer)
	}
	p = h.read(t, ports.TranscriptQuery{Before: p.FirstCursor, Limit: 4})
	if got := strings.Join(texts(p), ","); got != "1,2" || p.HasOlder || !p.HasNewer {
		t.Fatalf("oldest page = %s older=%v newer=%v", got, p.HasOlder, p.HasNewer)
	}
	first := p.FirstCursor
	p = h.read(t, ports.TranscriptQuery{Before: first, Limit: 4})
	if len(p.Entries) != 0 || p.Entries == nil || p.FirstCursor != "" || p.LastCursor != "" || p.HasOlder || !p.HasNewer {
		t.Fatalf("before the first entry = %+v", p)
	}

	p = h.read(t, ports.TranscriptQuery{After: first, Limit: 3})
	if got := strings.Join(texts(p), ","); got != "2,3,4" || !p.HasOlder || !p.HasNewer {
		t.Fatalf("after page = %s older=%v newer=%v", got, p.HasOlder, p.HasNewer)
	}
	last := h.read(t, ports.TranscriptQuery{}).LastCursor
	p = h.read(t, ports.TranscriptQuery{After: last})
	if len(p.Entries) != 0 || p.HasNewer || !p.HasOlder {
		t.Fatalf("after the last entry = %+v", p)
	}
}

func TestReadTranscriptOneLineManyEntries(t *testing.T) {
	h := newHome(t,
		assistant(map[string]any{"type": "text", "text": "a"}, toolUse("toolu_1", "Read", map[string]any{"file_path": "/x"}), map[string]any{"type": "text", "text": "b"}),
		userText("c"),
	)
	p := h.read(t, ports.TranscriptQuery{Limit: 2})
	if got := strings.Join(texts(p), ","); got != "b,c" {
		t.Fatalf("newest two = %s", got)
	}
	p = h.read(t, ports.TranscriptQuery{Before: p.FirstCursor, Limit: 1})
	if len(p.Entries) != 1 || p.Entries[0].Kind != "tool_use" || p.Entries[0].Text != "/x" {
		t.Fatalf("the middle part = %+v", p.Entries)
	}
}

// A tool_result is named after its tool_use even when that sits on an older
// page.
func TestReadTranscriptNamesToolResultsAcrossPages(t *testing.T) {
	h := newHome(t, assistant(toolUse("toolu_1", "Bash", map[string]any{"command": "ls"})), toolResult("toolu_1", "a b", false))
	p := h.read(t, ports.TranscriptQuery{Limit: 1})
	if len(p.Entries) != 1 || p.Entries[0].Kind != "tool_result" || p.Entries[0].Tool != "Bash" {
		t.Fatalf("tool result = %+v, want tool Bash", p.Entries)
	}
}

func TestReadTranscriptFollowsAppendsWithStableCursors(t *testing.T) {
	h := newHome(t, numbered(3)...)
	p := h.read(t, ports.TranscriptQuery{})
	cursors := map[string]string{}
	for _, e := range p.Entries {
		cursors[e.Text] = e.Cursor
	}

	// Half a line: not an entry until its newline arrives.
	line := userText("4")
	h.append(t, line[:10])
	if p := h.read(t, ports.TranscriptQuery{After: p.LastCursor}); len(p.Entries) != 0 {
		t.Fatalf("a half-written line became %+v", p.Entries)
	}
	h.append(t, line[10:]+"\n"+userText("5")+"\n")
	p = h.read(t, ports.TranscriptQuery{After: p.LastCursor})
	if got := strings.Join(texts(p), ","); got != "4,5" || p.HasNewer {
		t.Fatalf("after appending = %s newer=%v", got, p.HasNewer)
	}
	for _, e := range h.read(t, ports.TranscriptQuery{}).Entries {
		if c, ok := cursors[e.Text]; ok && c != e.Cursor {
			t.Errorf("cursor of %s moved from %s to %s", e.Text, c, e.Cursor)
		}
	}
}

func TestReadTranscriptReindexesARewrittenFile(t *testing.T) {
	h := newHome(t, numbered(5)...)
	h.read(t, ports.TranscriptQuery{})
	h.write(t, userText("only"))
	p := h.read(t, ports.TranscriptQuery{})
	if got := strings.Join(texts(p), ","); got != "only" {
		t.Fatalf("after shrinking = %s", got)
	}

	// Replaced by a new file of the same size.
	tmp := h.file + ".new"
	if err := os.WriteFile(tmp, []byte(userText("anew")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, h.file); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(texts(h.read(t, ports.TranscriptQuery{})), ","); got != "anew" {
		t.Fatalf("after replacing = %s", got)
	}
}

func TestReadTranscriptHandlesHugeLines(t *testing.T) {
	big := toolResult("toolu_1", []any{map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", 2<<20)}}}, false)
	h := newHome(t, userText("before"), big, userText("after"))
	if got := strings.Join(texts(h.read(t, ports.TranscriptQuery{})), ","); got != "before,[image],after" {
		t.Fatalf("entries = %s", got)
	}
}

func TestReadTranscriptRejectsUnknownCursors(t *testing.T) {
	h := newHome(t, numbered(2)...)
	p := h.read(t, ports.TranscriptQuery{})
	for _, c := range []string{"x", "1", "-1.0", "0.-1", "99999.0", p.FirstCursor + "0"} {
		for _, q := range []ports.TranscriptQuery{{Before: c, Limit: 10}, {After: c, Limit: 10}} {
			if _, err := h.r.ReadTranscript(h.work, "c1", q); code(err) != "INVALID_INPUT" {
				t.Errorf("cursor %q: %v, want INVALID_INPUT", c, err)
			}
		}
	}
}

// The worktree is gone: the transcript is found by its id alone.
func TestReadTranscriptAfterTheWorktreeIsGone(t *testing.T) {
	h := newHome(t, userText("kept"))
	if err := os.RemoveAll(h.work); err != nil {
		t.Fatal(err)
	}
	r := NewTranscriptReader(h.dir) // nothing cached
	p, err := r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{Limit: 10})
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("read = %+v, %v", p, err)
	}
	if _, err := r.ReadTranscript("", "c1", ports.TranscriptQuery{Limit: 10}); err != nil {
		t.Errorf("read without a directory: %v", err)
	}
}

func TestReadTranscriptOnlyReadsInsideProjects(t *testing.T) {
	h := newHome(t, userText("x"))
	outside := filepath.Join(t.TempDir(), "secret.jsonl")
	if err := os.WriteFile(outside, []byte(userText("secret")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(filepath.Dir(h.file), "c2.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.r.ReadTranscript(h.work, "c2", ports.TranscriptQuery{Limit: 10}); code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("symlink out of projects: %v, want SESSION_TRANSCRIPT_MISSING", err)
	}
	for _, id := range []string{"", "../c1", "c1/../c1", ".", "-c1", "c1.jsonl", "a/b"} {
		if _, err := h.r.ReadTranscript(h.work, id, ports.TranscriptQuery{Limit: 10}); code(err) != "SESSION_TRANSCRIPT_MISSING" {
			t.Errorf("id %q: %v, want SESSION_TRANSCRIPT_MISSING", id, err)
		}
	}
	if _, err := h.r.ReadTranscript(h.work, "c3", ports.TranscriptQuery{Limit: 10}); code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("missing conversation: %v", err)
	}
	if _, err := NewTranscriptReader(t.TempDir()).ReadTranscript(h.work, "c1", ports.TranscriptQuery{Limit: 10}); code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("no projects directory: %v", err)
	}
}

func TestReadTranscriptNeverChangesTheFile(t *testing.T) {
	h := newHome(t, numbered(20)...)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(h.file, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(h.file, 0o444); err != nil { // a write would fail
		t.Fatal(err)
	}
	before, _ := os.ReadFile(h.file)
	p := h.read(t, ports.TranscriptQuery{Limit: 5})
	h.read(t, ports.TranscriptQuery{Before: p.FirstCursor, Limit: 5})
	h.read(t, ports.TranscriptQuery{After: p.FirstCursor, Limit: 5})
	after, _ := os.ReadFile(h.file)
	fi, _ := os.Stat(h.file)
	if string(before) != string(after) || !fi.ModTime().Equal(old) || fi.Mode().Perm() != 0o444 {
		t.Errorf("the transcript changed: mtime %v, mode %v", fi.ModTime(), fi.Mode())
	}
}

func TestReadTranscriptEvictsOldIndexes(t *testing.T) {
	h := newHome(t, userText("x"))
	for i := range maxIndexes + 5 {
		id := fmt.Sprintf("conv%d", i)
		if err := os.WriteFile(filepath.Join(filepath.Dir(h.file), id+".jsonl"), []byte(userText(id)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := h.r.ReadTranscript(h.work, id, ports.TranscriptQuery{Limit: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(h.r.indexes); n != maxIndexes {
		t.Errorf("%d indexes kept, want %d", n, maxIndexes)
	}
}

// bigHome is a transcript like a long real session: tool calls with large
// results and a few images, about 20 MB.
func bigHome(b *testing.B) *home {
	b.Helper()
	var lines []string
	for i := range 1500 {
		id := fmt.Sprintf("toolu_%d", i)
		lines = append(lines, assistant(toolUse(id, "Bash", map[string]any{"command": "go test ./..."})))
		lines = append(lines, toolResult(id, strings.Repeat("output line\n", 800), false))
		if i%100 == 0 {
			lines = append(lines, toolResult(id, []any{map[string]any{"type": "image", "source": map[string]any{"data": strings.Repeat("A", 512<<10)}}}, false))
		}
	}
	return newHome(b, lines...)
}

func BenchmarkTranscriptPollUnchanged(b *testing.B) {
	h := bigHome(b)
	p, err := h.r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{Limit: 200})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{After: p.LastCursor, Limit: 200}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranscriptPageBack(b *testing.B) {
	h := bigHome(b)
	p, err := h.r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{Limit: 200})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{Before: p.FirstCursor, Limit: 200}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTranscriptFirstIndex(b *testing.B) {
	h := bigHome(b)
	for b.Loop() {
		r := NewTranscriptReader(h.dir)
		if _, err := r.ReadTranscript(h.work, "c1", ports.TranscriptQuery{Limit: 200}); err != nil {
			b.Fatal(err)
		}
	}
}

// put writes conversation id holding lines for work under config dir dir.
func put(t *testing.T, dir, work, id string, lines ...string) {
	t.Helper()
	real, _ := filepath.EvalSymlinks(work)
	proj := filepath.Join(dir, "projects", nonAlnum.ReplaceAllString(real, "-"))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadTranscriptFallsBackToTheLegacyHomeAndPrefersTheAgentHome(t *testing.T) {
	agent, legacy := t.TempDir(), t.TempDir()
	work := filepath.Join(t.TempDir(), "agent-x")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, legacy, work, "old", userText("from legacy"))
	put(t, legacy, work, "both", userText("legacy copy"))
	put(t, agent, work, "both", userText("agent copy"))
	put(t, agent, work, "new", userText("from agent"))
	r := &TranscriptReader{Dir: agent, Legacy: legacy}

	for id, want := range map[string]string{"old": "from legacy", "new": "from agent", "both": "agent copy"} {
		p, err := r.ReadTranscript(work, id, ports.TranscriptQuery{Limit: 10})
		if err != nil || len(p.Entries) != 1 || p.Entries[0].Text != want {
			t.Errorf("%s: %+v, %v; want %q", id, p, err, want)
		}
	}
	if _, err := r.ReadTranscript(work, "none", ports.TranscriptQuery{Limit: 10}); code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("unknown: %v", err)
	}
	// Found by id alone once the worktree is gone, in the legacy home too.
	if _, err := r.ReadTranscript("", "old", ports.TranscriptQuery{Limit: 10}); err != nil {
		t.Errorf("legacy without a directory: %v", err)
	}
}
