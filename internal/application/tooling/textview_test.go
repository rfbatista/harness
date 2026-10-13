package tooling

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTextView_HTMLRendering(t *testing.T) {
	for _, tc := range []struct {
		name, html string
		want       []string
	}{
		{"head, style, script, template and comments are dropped",
			`<!doctype html><html><head><title>T</title><style>p{color:red}</style></head><body>` +
				`<script>var x = "<p>no</p>";</script><template><p>no</p></template><!-- no --><p>yes</p></body></html>`,
			[]string{"yes"}},
		{"one line per block, whitespace collapsed",
			"<body><p>one\n   two</p><div>three</div>four<br>five</body>",
			[]string{"one two", "three", "four", "five"}},
		{"headings carry their level",
			"<body><h1>Top</h1><h2>Mid</h2><h6>Low</h6></body>",
			[]string{"# Top", "## Mid", "###### Low"}},
		{"nested lists",
			"<ul><li>a<ul><li>a1</li><li>a2</li></ul></li><li>b</li></ul>",
			[]string{"a", "a1", "a2", "b"}},
		{"table rows join their cells",
			"<table><thead><tr><th>Kind</th><th>Where</th></tr></thead><tr><td>x</td><td><code>y</code> z</td></tr></table>",
			[]string{"Kind | Where", "x | y z"}},
		{"pre keeps its lines, blank ones too, but not the newline after the tag",
			"<pre>\nfunc a() {\n\n    return\n}\n</pre><p>after</p>",
			[]string{"func a() {", "", "    return", "}", "after"}},
		{"entities are decoded",
			"<p>a &lt;b&gt; &amp; c&nbsp;d &mdash; e</p>",
			[]string{"a <b> & c d — e"}},
		{"definition lists",
			"<dl><dt>Agent</dt><dd>go-developer</dd></dl>",
			[]string{"Agent", "go-developer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := newTextView(tc.html, textHTML).lines; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lines\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func outlineOf(v *textView) []string {
	out := make([]string, len(v.outline))
	for i, s := range v.outline {
		out[i] = fmt.Sprintf("%s L%d %q %d-%d", s.ID, s.Level, s.Heading, s.Start, s.End)
	}
	return out
}

func TestTextView_HTMLOutline(t *testing.T) {
	page := `<body><h1>Plan</h1><p>intro</p>` +
		`<h2 id="where">Where</h2><p>w</p>` +
		`<h3>Detail</h3><p>d</p>` +
		`<h2>Slices</h2><h4>One</h4><p>1</p><h4>Two</h4><p>2</p>` +
		`<h5>not outlined</h5><h2>Risks</h2><p>r</p></body>`
	v := newTextView(page, textHTML)
	want := []string{
		`1 L1 "Plan" 1-14`,
		`where L2 "Where" 3-6`,
		`1.1.1 L3 "Detail" 5-6`,
		`1.2 L2 "Slices" 7-12`,
		`1.2.1 L4 "One" 8-9`,
		`1.2.2 L4 "Two" 10-12`,
		`1.3 L2 "Risks" 13-14`,
	}
	if got := outlineOf(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("outline\n got %q\nwant %q\nlines %q", got, want, v.lines)
	}
}

// A page with fewer than two headings is outlined by its dt terms too, at
// level 5 under the heading before them.
func TestTextView_DtFallback(t *testing.T) {
	spec := `<body><h1>Harness server</h1><dl><dt>Agent</dt><dd>go</dd><dt>Why</dt><dd>because</dd><dd>and more</dd></dl></body>`
	want := []string{`1 L1 "Harness server" 1-6`, `1.1 L5 "Agent" 2-3`, `1.2 L5 "Why" 4-6`}
	if got := outlineOf(newTextView(spec, textHTML)); !reflect.DeepEqual(got, want) {
		t.Fatalf("outline\n got %q\nwant %q", got, want)
	}
	bare := `<body><dl><dt>A</dt><dd>a</dd><dt>B</dt><dd>b</dd></dl></body>`
	if got := outlineOf(newTextView(bare, textHTML)); !reflect.DeepEqual(got, []string{`1 L5 "A" 1-2`, `2 L5 "B" 3-4`}) {
		t.Fatalf("outline without headings: %q", got)
	}
	headed := `<body><h1>A</h1><h2>B</h2><dl><dt>x</dt><dd>y</dd></dl></body>`
	for _, s := range newTextView(headed, textHTML).outline {
		if s.Level == 5 {
			t.Fatalf("a page with two headings has dt sections: %+v", s)
		}
	}
}

func TestTextView_Markdown(t *testing.T) {
	md := "# Title\r\nintro\n\n## Part ##\n```\n# not a heading\n```\ntext\n##### too deep\n### Sub\nend\n"
	v := newTextView(md, textMarkdown)
	if len(v.lines) != 11 || v.lines[0] != "# Title" || v.lines[10] != "end" {
		t.Fatalf("lines %q", v.lines)
	}
	want := []string{`1 L1 "Title" 1-11`, `1.1 L2 "Part" 4-11`, `1.1.1 L3 "Sub" 10-11`}
	if got := outlineOf(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("outline\n got %q\nwant %q", got, want)
	}
	if raw := rawView("a\r\nb\n"); !reflect.DeepEqual(raw.lines, []string{"a\r", "b"}) || raw.outline != nil {
		t.Fatalf("raw view %q", raw.lines)
	}
}

func TestTextView_Section(t *testing.T) {
	v := newTextView(`<body><h1>Plan</h1><h2 id="risks">Risks</h2><h2>Slice</h2><h2>Slice</h2></body>`, textHTML)
	for key, want := range map[string]string{"risks": "Risks", "1": "Plan", "1.3": "Slice", "  RISKS ": "Risks", "plan": "Plan"} {
		s, err := v.section(key)
		if err != nil || s.Heading != want {
			t.Fatalf("section(%q) = %+v, %v", key, s, err)
		}
	}
	_, err := v.section("nope")
	wantCode(t, err, "SECTION_NOT_FOUND")
	if !strings.Contains(err.Error(), "sections: 1, risks, 1.2, 1.3") {
		t.Fatalf("the miss does not list the sections: %v", err)
	}
	_, err = v.section("slice")
	wantCode(t, err, "SECTION_AMBIGUOUS")
	if !strings.Contains(err.Error(), "use one of: 1.2, 1.3") {
		t.Fatalf("the ambiguity does not list the candidates: %v", err)
	}
}

func TestTextView_WindowBudgetAndCut(t *testing.T) {
	long := strings.Repeat("é", maxLineChars+5)
	v := &textView{lines: []string{"a", long, "c", "d"}}
	got, next := v.window(1, 4, 10, budgetBytes)
	if next != 0 || len(got) != 4 || got[1].N != 2 {
		t.Fatalf("window %+v next %d", got, next)
	}
	if want := strings.Repeat("é", maxLineChars) + "… [+5 chars]"; got[1].Text != want {
		t.Fatalf("cut line has %d runes: %q…", utf8.RuneCountInString(got[1].Text), got[1].Text[len(got[1].Text)-20:])
	}
	if got, next := v.window(2, 4, 2, budgetBytes); len(got) != 2 || got[0].N != 2 || next != 4 {
		t.Fatalf("limit: %+v next %d", got, next)
	}
	if got, next := v.window(1, 4, 10, 10); len(got) != 1 || next != 2 {
		t.Fatalf("budget: %+v next %d", got, next)
	}
	if got, next := v.window(2, 4, 10, 1); len(got) != 1 || got[0].N != 2 || next != 3 {
		t.Fatalf("a line over the budget still comes out alone: %+v next %d", got, next)
	}
	if got, next := v.window(9, 4, 10, budgetBytes); len(got) != 0 || next != 0 {
		t.Fatalf("past the end: %+v next %d", got, next)
	}
}

// Golden views of real pages: the System architecture page (about 48 KB, the
// page that started this) and a spec-shaped page. UPDATE_GOLDEN=1 rewrites them.
func TestTextView_Golden(t *testing.T) {
	for _, name := range []string{"architecture", "spec"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", "textview", name+".html"))
			if err != nil {
				t.Fatal(err)
			}
			v := newTextView(string(src), textHTML)
			var b strings.Builder
			for i, l := range v.lines {
				fmt.Fprintf(&b, "%4d  %s\n", i+1, cutLine(l))
			}
			b.WriteString("---- outline\n")
			for _, s := range outlineOf(v) {
				b.WriteString(s + "\n")
			}
			golden := filepath.Join("testdata", "textview", name+".golden")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(golden, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if b.String() != string(want) {
				t.Fatalf("the view of %s changed; rerun with UPDATE_GOLDEN=1 if that is intended", name)
			}
		})
	}
}

func FuzzTextView(f *testing.F) {
	for _, seed := range []string{
		`<!doctype html><html><head><style>x</style></head><body><h1 id="a">A</h1><pre>
x
</pre><table><tr><td>1<td>2</table><dl><dt>t<dd>d</dl></body></html>`,
		"<h2>a<h3>b</h2></h3><script>",
		"<pre><pre>\n\n</pre>",
		"# a\n```\n## b\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, kind := range []textKind{textHTML, textMarkdown} {
			v := newTextView(s, kind)
			again := newTextView(s, kind)
			if !reflect.DeepEqual(v, again) {
				t.Fatalf("not deterministic for %q", s)
			}
			for _, sec := range v.outline {
				if sec.Start < 1 || sec.Start > len(v.lines) || sec.End < sec.Start || sec.End > len(v.lines) {
					t.Fatalf("section %+v out of %d lines for %q", sec, len(v.lines), s)
				}
			}
		}
		if strings.Contains(s, "<style>") && !strings.Contains(s, "</style>") {
			return
		}
		for _, l := range newTextView("<body><style>SECRET{}</style>"+s+"</body>", textHTML).lines {
			if strings.Contains(l, "SECRET{}") {
				t.Fatalf("style text survived for %q", s)
			}
		}
	})
}
