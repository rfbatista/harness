package tooling

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// taskDoc creates an HTML document linked to the fixture's task.
func (f *taskToolsFixture) taskDoc(t *testing.T, title, content string) *domain.Document {
	t.Helper()
	return f.doc(t, "create_task_document", map[string]any{"title": title, "content": content})
}

// read calls a read tool and answers its JSON as a generic map, the way an
// agent sees it.
func (f *taskToolsFixture) read(t *testing.T, tool string, args map[string]any) map[string]any {
	t.Helper()
	out, err := f.call(t, f.sessionID, tool, args)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// paragraphs is a page of n one-line paragraphs, "p1" … "pn".
func paragraphs(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("<p>p")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("</p>\n")
	}
	return htmlPage("Doc", b.String())
}

func lineTexts(m map[string]any) []string {
	var out []string
	for _, l := range m["lines"].([]any) {
		out = append(out, l.(map[string]any)["text"].(string))
	}
	return out
}

func lineNumbers(m map[string]any) []int {
	var out []int
	for _, l := range m["lines"].([]any) {
		out = append(out, int(l.(map[string]any)["n"].(float64)))
	}
	return out
}

// A document under the guard, read with no window, answers what it always
// did, plus its size and version: nothing an existing caller relies on moves.
func TestReadDocument_UnderTheGuardIsTodaysAnswerPlusMetadata(t *testing.T) {
	f := newTaskToolsFixture(t)
	d := f.taskDoc(t, "Plan", htmlPage("Plan", "<h1>Plan</h1><p>step one</p>"))
	got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID})

	before, _ := json.Marshal(map[string]any{"document": f.plan.GetDocument(d.ID)})
	var want map[string]any
	_ = json.Unmarshal(before, &want)
	doc := got["document"].(map[string]any)
	if doc["bytes"] != float64(len(d.Content)) || doc["lines"] != float64(2) || doc["version"] != domain.DocumentVersion(d.Content) {
		t.Fatalf("metadata: %v", doc)
	}
	delete(doc, "bytes")
	delete(doc, "lines")
	delete(doc, "version")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the small read changed:\n got %v\nwant %v", got, want)
	}
}

// The guard is 24 KiB of stored content or 400 text lines, whichever comes
// first. Markup counts toward the bytes: a page whose text is small but whose
// style block is big is still too big to hand back whole.
func TestReadDocument_Guard(t *testing.T) {
	f := newTaskToolsFixture(t)
	padTo := func(n int) string {
		base := htmlPage("Doc", "<style></style><p>short</p>")
		return strings.Replace(base, "<style></style>", "<style>"+strings.Repeat("a", n-len(base))+"</style>", 1)
	}
	for _, tc := range []struct {
		name    string
		content string
		whole   bool
	}{
		{"24 KiB exactly", padTo(guardBytes), true},
		{"24 KiB + 1", padTo(guardBytes + 1), false},
		{"400 lines", paragraphs(guardLines), true},
		{"401 lines", paragraphs(guardLines + 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := f.taskDoc(t, tc.name, tc.content)
			got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID})
			_, hasContent := got["document"].(map[string]any)["content"]
			if hasContent != tc.whole || (got["lines"] == nil) == !tc.whole {
				t.Fatalf("whole = %v, want %v: %v", hasContent, tc.whole, keys(got))
			}
		})
	}
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Over the guard, a read with no window answers the outline and the first
// window, without the content.
func TestReadDocument_OverTheGuardAnswersOutlineAndFirstWindow(t *testing.T) {
	f := newTaskToolsFixture(t)
	body := "<h1>Big</h1>" + strings.Repeat("<p>filler</p>", 300) + `<h2 id="tail">Tail</h2>` + strings.Repeat("<p>more</p>", 150)
	d := f.taskDoc(t, "Big", htmlPage("Big", body))
	got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID})

	doc := got["document"].(map[string]any)
	if _, ok := doc["content"]; ok || doc["id"] != d.ID || doc["lines"] != float64(452) || doc["version"] != domain.DocumentVersion(d.Content) {
		t.Fatalf("document: %v", doc)
	}
	if got["truncated"] != true || got["next_offset"] != float64(201) || got["total_lines"] != float64(452) || got["budget_bytes"] != float64(budgetBytes) {
		t.Fatalf("window: truncated %v next %v total %v", got["truncated"], got["next_offset"], got["total_lines"])
	}
	if g := got["guard"].(map[string]any); g["bytes"] != float64(guardBytes) || g["lines"] != float64(guardLines) {
		t.Fatalf("guard: %v", g)
	}
	if n := lineNumbers(got); len(n) != 200 || n[0] != 1 || n[199] != 200 {
		t.Fatalf("first window: %d lines", len(n))
	}
	outline := got["outline"].([]any)
	if len(outline) != 2 || outline[1].(map[string]any)["section_id"] != "tail" || outline[1].(map[string]any)["line_start"] != float64(302) {
		t.Fatalf("outline: %v", outline)
	}
}

func TestReadDocument_Windows(t *testing.T) {
	f := newTaskToolsFixture(t)
	d := f.taskDoc(t, "Doc", paragraphs(10))

	got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "offset": 4, "limit": 3})
	if !reflect.DeepEqual(lineTexts(got), []string{"p4", "p5", "p6"}) || got["next_offset"] != float64(7) || got["truncated"] != true {
		t.Fatalf("window: %v next %v", lineTexts(got), got["next_offset"])
	}
	if _, ok := got["outline"]; ok {
		t.Fatal("a window repeats the outline")
	}
	if _, ok := got["document"].(map[string]any)["content"]; ok {
		t.Fatal("a window carries the content")
	}
	got = f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "offset": 9})
	if !reflect.DeepEqual(lineTexts(got), []string{"p9", "p10"}) || got["next_offset"] != nil || got["truncated"] != false {
		t.Fatalf("last window: %v next %v", lineTexts(got), got["next_offset"])
	}
	got = f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "offset": 99})
	if len(got["lines"].([]any)) != 0 || got["next_offset"] != nil {
		t.Fatalf("past the end: %v", got)
	}
}

// A window stops at the budget, whatever the limit, and says where to go on.
func TestReadDocument_BudgetStopsAWindow(t *testing.T) {
	f := newTaskToolsFixture(t)
	d := f.taskDoc(t, "Wide", htmlPage("Wide", strings.Repeat("<p>"+strings.Repeat("w", 1000)+"</p>", 100)))
	got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "limit": 100})
	n := lineNumbers(got)
	if len(n) != budgetBytes/1000 || got["next_offset"] != float64(len(n)+1) {
		t.Fatalf("budget: %d lines, next %v", len(n), got["next_offset"])
	}
}

// Long lines are cut in results; the raw view shows the stored markup.
func TestReadDocument_RawViewAndCutLines(t *testing.T) {
	f := newTaskToolsFixture(t)
	content := htmlPage("Doc", "<p>"+strings.Repeat("x", 2500)+"</p>")
	d := f.taskDoc(t, "Doc", content)
	raw := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "view": "raw"})
	if want := strings.Split(content, "\n"); len(lineTexts(raw)) != len(want) || lineTexts(raw)[0] != want[0] {
		t.Fatalf("raw: %q", lineTexts(raw))
	}
	text := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "offset": 1})
	if l := lineTexts(text)[0]; l != strings.Repeat("x", 2000)+"… [+500 chars]" {
		t.Fatalf("cut line ends %q", l[len(l)-20:])
	}
}

func TestReadDocument_Sections(t *testing.T) {
	f := newTaskToolsFixture(t)
	body := `<h1>Plan</h1><p>intro</p><h2 id="where">Where</h2><p>w1</p><p>w2</p><p>w3</p><h2>Risks</h2><p>r</p><h2>Risks</h2><p>r2</p>`
	d := f.taskDoc(t, "Plan", htmlPage("Plan", body))
	for key, want := range map[string][]string{
		"where": {"## Where", "w1", "w2", "w3"},
		"1.2":   {"## Risks", "r"},
		"WHERE": {"## Where", "w1", "w2", "w3"},
	} {
		got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "section": key})
		if !reflect.DeepEqual(lineTexts(got), want) || got["next_offset"] != nil {
			t.Fatalf("section %q: %v", key, lineTexts(got))
		}
		if _, ok := got["outline"]; ok {
			t.Fatalf("section %q repeats the outline", key)
		}
	}
	// offset and limit page within the section, relative to it.
	got := f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "section": "where", "offset": 2, "limit": 2})
	if !reflect.DeepEqual(lineTexts(got), []string{"w1", "w2"}) || !reflect.DeepEqual(lineNumbers(got), []int{4, 5}) || got["next_offset"] != float64(4) {
		t.Fatalf("paging a section: %v %v next %v", lineTexts(got), lineNumbers(got), got["next_offset"])
	}
	got = f.read(t, "read_task_document", map[string]any{"document_id": d.ID, "section": "where", "offset": 4})
	if !reflect.DeepEqual(lineTexts(got), []string{"w3"}) || got["next_offset"] != nil {
		t.Fatalf("the section's last page: %v next %v", lineTexts(got), got["next_offset"])
	}

	for args, code := range map[string]string{
		`{"section":"nope"}`:               "SECTION_NOT_FOUND",
		`{"section":"risks"}`:              "SECTION_AMBIGUOUS",
		`{"section":"where","view":"raw"}`: "INVALID_INPUT",
		`{"limit":0}`:                      "INVALID_INPUT",
		`{"limit":1001}`:                   "INVALID_INPUT",
		`{"offset":0}`:                     "INVALID_INPUT",
		`{"offset":1.5}`:                   "INVALID_INPUT",
		`{"view":"html"}`:                  "INVALID_INPUT",
	} {
		var a map[string]any
		_ = json.Unmarshal([]byte(args), &a)
		a["document_id"] = d.ID
		_, err := f.call(t, f.sessionID, "read_task_document", a)
		wantCode(t, err, code)
		if !strings.HasPrefix(err.(*domain.StructuredError).Message, code+": ") {
			t.Fatalf("%s: message not coded: %v", args, err)
		}
	}
	_, err := f.call(t, f.sessionID, "read_task_document", map[string]any{"document_id": d.ID, "section": "nope"})
	if !strings.Contains(err.Error(), "sections: 1, where, 1.2, 1.3") {
		t.Fatalf("the miss does not list the sections: %v", err)
	}
}

// Project documents read the same way, and Markdown ones too.
func TestReadDocument_ProjectAndMarkdown(t *testing.T) {
	f := newTaskToolsFixture(t)
	md := "# Notes\n\n## A\none\n## B\ntwo\nthree\n"
	d, _ := f.plan.CreateDocument(f.projectID, "Notes", md, domain.DocumentFormatMarkdown, domain.DocumentScopeProject)
	got := f.read(t, "read_project_document", map[string]any{"document_id": d.ID, "section": "B"})
	if !reflect.DeepEqual(lineTexts(got), []string{"## B", "two", "three"}) || got["document"].(map[string]any)["format"] != "markdown" {
		t.Fatalf("markdown section: %v", got)
	}
	whole := f.read(t, "read_project_document", map[string]any{"document_id": d.ID})
	if whole["document"].(map[string]any)["content"] != md || whole["document"].(map[string]any)["lines"] != float64(7) {
		t.Fatalf("small markdown read: %v", whole)
	}
}
