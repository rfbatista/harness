package tooling

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func (f *taskToolsFixture) search(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	return f.read(t, "search_documents", args)
}

type hit struct {
	source, id, text, section string
	line                      int
	before, after             []string
}

func hits(m map[string]any) []hit {
	var out []hit
	for _, raw := range m["matches"].([]any) {
		x := raw.(map[string]any)
		h := hit{source: x["source"].(string), id: x["id"].(string), text: x["text"].(string), line: int(x["line"].(float64))}
		if s, ok := x["section_id"].(string); ok {
			h.section = s
		}
		for _, l := range x["before"].([]any) {
			h.before = append(h.before, l.(string))
		}
		for _, l := range x["after"].([]any) {
			h.after = append(h.after, l.(string))
		}
		out = append(out, h)
	}
	return out
}

func TestSearchDocuments_LiteralRegexCaseAndContext(t *testing.T) {
	f := newTaskToolsFixture(t)
	d := f.taskDoc(t, "Contract", htmlPage("Contract", `<h1>Contract</h1><p>intro</p><h2 id="errors">Errors</h2>`+
		`<p>before</p><p>ARTIFACT_NOT_FOUND when gone</p><p>after</p><p>artifact_not_found again (a.b)</p>`))

	got := hits(f.search(t, map[string]any{"pattern": "artifact_not_found"}))
	if len(got) != 2 || got[0].line != 5 || got[0].text != "ARTIFACT_NOT_FOUND when gone" || got[0].section != "errors" ||
		!reflect.DeepEqual(got[0].before, []string{"before"}) || !reflect.DeepEqual(got[0].after, []string{"after"}) || got[0].source != "task_document" || got[0].id != d.ID {
		t.Fatalf("case-insensitive literal: %+v", got)
	}
	if got := hits(f.search(t, map[string]any{"pattern": "artifact_not_found", "case_sensitive": true})); len(got) != 1 || got[0].line != 7 {
		t.Fatalf("case-sensitive: %+v", got)
	}
	if got := hits(f.search(t, map[string]any{"pattern": "(a.b)"})); len(got) != 1 || got[0].line != 7 {
		t.Fatalf("a literal pattern is not a regex: %+v", got)
	}
	if got := hits(f.search(t, map[string]any{"pattern": `ARTIFACT_\w+ when`, "regex": true, "context": 0})); len(got) != 1 || len(got[0].before) != 0 || len(got[0].after) != 0 {
		t.Fatalf("regex with no context: %+v", got)
	}
	if got := hits(f.search(t, map[string]any{"pattern": `<p>before`, "view": "raw"})); len(got) != 1 || got[0].section != "" {
		t.Fatalf("raw view: %+v", got)
	}
	for code, args := range map[string]map[string]any{
		"INVALID_PATTERN": {"pattern": "(?<=x)y", "regex": true},
		"INVALID_INPUT":   {"pattern": ""},
	} {
		_, err := f.call(t, f.sessionID, "search_documents", args)
		wantCode(t, err, code)
		if !strings.HasPrefix(err.(*domain.StructuredError).Message, code+": ") {
			t.Fatalf("message not coded: %v", err)
		}
	}
	for _, bad := range []map[string]any{
		{"pattern": "x", "context": 6}, {"pattern": "x", "max_matches": 0}, {"pattern": "x", "max_matches": 201},
		{"pattern": "x", "scope": "everything"}, {"pattern": "x", "view": "html"}, {"pattern": strings.Repeat("x", 513)},
	} {
		_, err := f.call(t, f.sessionID, "search_documents", bad)
		wantCode(t, err, "INVALID_INPUT")
	}
}

// Search sees what the read tools see, and nothing else: this task's
// documents and the project's. A document in both is reported once.
func TestSearchDocuments_ScopeAndNoLeaks(t *testing.T) {
	f := newTaskToolsFixture(t)
	mine := f.taskDoc(t, "Mine", htmlPage("Mine", "<p>needle in mine</p>"))
	shared := f.taskDoc(t, "Shared", htmlPage("Shared", "<p>needle in shared</p>"))
	if _, err := f.plan.SetDocumentScope(shared.ID, domain.DocumentScopeProject); err != nil {
		t.Fatal(err)
	}
	conv, _ := f.plan.CreateDocument(f.projectID, "Conventions", "# needle in conventions", domain.DocumentFormatMarkdown, domain.DocumentScopeProject)
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Elsewhere", "", domain.TicketStatusTodo)
	theirs, _ := f.plan.CreateDocument(f.projectID, "Theirs", "needle in theirs", "", domain.DocumentScopeTask)
	_ = f.plan.LinkDocument(other.ID, theirs.ID)
	p2, _ := f.projects.Create("p2", t.TempDir())
	_, _ = f.plan.CreateDocument(p2.ID, "Foreign", "needle in foreign", "", domain.DocumentScopeProject)

	ids := func(m map[string]any) map[string]string {
		out := map[string]string{}
		for _, h := range hits(m) {
			if _, dup := out[h.id]; dup {
				t.Fatalf("%s reported twice", h.id)
			}
			out[h.id] = h.source
		}
		return out
	}
	all := f.search(t, map[string]any{"pattern": "needle"})
	if got := ids(all); !reflect.DeepEqual(got, map[string]string{mine.ID: "task_document", shared.ID: "project_document", conv.ID: "project_document"}) {
		t.Fatalf("scope all: %v", got)
	}
	if all["searched"] != float64(3) || all["skipped"] != float64(0) || all["truncated"] != false {
		t.Fatalf("counts: %v", all)
	}
	if got := ids(f.search(t, map[string]any{"pattern": "needle", "scope": "task"})); !reflect.DeepEqual(got, map[string]string{mine.ID: "task_document", shared.ID: "project_document"}) {
		t.Fatalf("scope task: %v", got)
	}
	if got := ids(f.search(t, map[string]any{"pattern": "needle", "scope": "project"})); !reflect.DeepEqual(got, map[string]string{shared.ID: "project_document", conv.ID: "project_document"}) {
		t.Fatalf("scope project: %v", got)
	}
	if got := ids(f.search(t, map[string]any{"pattern": "needle", "document_id": conv.ID})); len(got) != 1 {
		t.Fatalf("one document: %v", got)
	}
	_, err := f.call(t, f.sessionID, "search_documents", map[string]any{"pattern": "needle", "document_id": theirs.ID})
	wantCode(t, err, "DOCUMENT_NOT_ON_TASK")
}

func TestSearchDocuments_CapsAndBudget(t *testing.T) {
	f := newTaskToolsFixture(t)
	f.taskDoc(t, "Many", paragraphs(300))
	m := f.search(t, map[string]any{"pattern": "p", "max_matches": 10})
	if len(hits(m)) != 10 || m["truncated"] != true {
		t.Fatalf("max_matches: %d, truncated %v", len(hits(m)), m["truncated"])
	}
	if m := f.search(t, map[string]any{"pattern": "p1", "max_matches": 200}); m["truncated"] != false || len(hits(m)) != 111 {
		t.Fatalf("under the cap: %d, truncated %v", len(hits(m)), m["truncated"])
	}
	f.taskDoc(t, "Wide", htmlPage("Wide", strings.Repeat("<p>wide "+strings.Repeat("w", 1900)+"</p>", 60)))
	m = f.search(t, map[string]any{"pattern": "wide", "max_matches": 200, "context": 0})
	if n := len(hits(m)); n == 0 || n*1900 > budgetBytes || m["truncated"] != true {
		t.Fatalf("budget: %d matches, truncated %v", n, m["truncated"])
	}
}

// A match far along a line too long to show comes back as an excerpt around it.
func TestSearchDocuments_ExcerptOnLongLines(t *testing.T) {
	f := newTaskToolsFixture(t)
	f.taskDoc(t, "Long", htmlPage("Long", "<p>"+strings.Repeat("a", 3000)+"NEEDLE"+strings.Repeat("b", 3000)+"</p>"))
	got := hits(f.search(t, map[string]any{"pattern": "needle"}))
	if len(got) != 1 {
		t.Fatalf("hits: %+v", got)
	}
	want := "…" + strings.Repeat("a", 100) + "NEEDLE" + strings.Repeat("b", 100) + "…"
	if got[0].text != want {
		t.Fatalf("excerpt %q", got[0].text)
	}
	f.taskDoc(t, "Greedy", htmlPage("Greedy", "<p>"+strings.Repeat("z", 5000)+"</p>"))
	got = hits(f.search(t, map[string]any{"pattern": "z+", "regex": true}))
	if len(got) != 1 || got[0].text != strings.Repeat("z", excerptChars)+"…" {
		t.Fatalf("a long match shows its start: %d chars", len(got[0].text))
	}
}

// A hit's line number is the line a window read at that offset returns.
func TestSearchDocuments_HitsLineUpWithReads(t *testing.T) {
	f := newTaskToolsFixture(t)
	src, err := os.ReadFile(filepath.Join("testdata", "textview", "architecture.html"))
	if err != nil {
		t.Fatal(err)
	}
	d := f.taskDoc(t, "Architecture", string(src))
	for _, view := range []string{"text", "raw"} {
		for _, h := range hits(f.search(t, map[string]any{"pattern": "port", "view": view, "max_matches": 200, "context": 0})) {
			w := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "offset": h.line, "limit": 1, "view": view})
			line := lineTexts(w)[0]
			excerpt := strings.HasPrefix(h.text, "…") || strings.HasSuffix(h.text, "…")
			if !strings.Contains(strings.ToLower(line), "port") || (!excerpt && line != h.text) {
				t.Fatalf("%s line %d: search %q, read %q", view, h.line, h.text, line)
			}
		}
	}
}
