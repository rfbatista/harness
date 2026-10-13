package tooling

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"operators-mcp/internal/domain"
)

// A text view is how the session tools let an agent read a document or a
// text design asset in parts: numbered lines and an outline of sections, so
// it can read a window, a section, or a search hit instead of the whole page.
// Reads, search and the listings all build their views here, so a line
// number from one is valid in the others for the same content.

const (
	// maxLineChars is the longest line a result carries; a longer one is cut
	// with "… [+N chars]". The view keeps the whole line, so numbering and
	// search see all of it.
	maxLineChars = 2000
	// budgetBytes caps the line text in a single result.
	budgetBytes = 32 << 10
	// guardBytes and guardLines are the guard: a document over either is
	// answered with its outline and a first window instead of its content.
	guardBytes = 24 << 10
	guardLines = 400
)

// textKind says how content becomes a view.
type textKind int

const (
	textPlain    textKind = iota // lines as stored
	textMarkdown                 // lines as stored, outlined from # headings
	textHTML                     // rendered: markup dropped, one line per block
)

// section is one entry of a view's outline. Start and End are the 1-based
// lines it covers, End inclusive.
type section struct {
	ID      string `json:"section_id"`
	Level   int    `json:"level"`
	Heading string `json:"heading"`
	Start   int    `json:"line_start"`
	End     int    `json:"line_end"`
}

// viewLine is a numbered line as a result carries it.
type viewLine struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

type textView struct {
	lines   []string
	outline []section
}

// documentKind is the view a document's format calls for.
func documentKind(d *domain.Document) textKind {
	if d.Format == domain.DocumentFormatHTML {
		return textHTML
	}
	return textMarkdown
}

// newTextView builds the text view of content.
func newTextView(content string, kind textKind) *textView {
	switch kind {
	case textHTML:
		return renderHTML(content)
	case textMarkdown:
		v := &textView{lines: splitLines(strings.ReplaceAll(content, "\r\n", "\n"))}
		v.outline = markdownOutline(v.lines)
		return v
	default:
		return &textView{lines: splitLines(strings.ReplaceAll(content, "\r\n", "\n"))}
	}
}

// rawView is the stored content split on "\n", with no outline.
func rawView(content string) *textView {
	return &textView{lines: splitLines(content)}
}

// splitLines splits on "\n"; a final newline does not start an empty line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// window returns the lines first..last (1-based, inclusive), at most limit of
// them and at most budget bytes of text, every line cut to maxLineChars. next
// is the line to continue from, or 0 when the range is done. At least one line
// is returned when the range is not empty, whatever the budget.
func (v *textView) window(first, last, limit, budget int) (out []viewLine, next int) {
	if first < 1 {
		first = 1
	}
	if last > len(v.lines) {
		last = len(v.lines)
	}
	out = []viewLine{}
	used := 0
	for n := first; n <= last; n++ {
		text := cutLine(v.lines[n-1])
		if len(out) == limit || (len(out) > 0 && used+len(text) > budget) {
			return out, n
		}
		used += len(text)
		out = append(out, viewLine{N: n, Text: text})
	}
	return out, 0
}

// cutLine shortens a line over maxLineChars, saying how much was left out.
func cutLine(s string) string {
	if utf8.RuneCountInString(s) <= maxLineChars {
		return s
	}
	cut := 0
	for i := range s {
		if cut == maxLineChars {
			return s[:i] + "… [+" + strconv.Itoa(utf8.RuneCountInString(s[i:])) + " chars]"
		}
		cut++
	}
	return s
}

// --- Outline -----------------------------------------------------------------

// heading is an outline entry before its section id and end are known.
type heading struct {
	level int
	id    string // the id attribute, if any
	text  string
	line  int
}

// buildOutline numbers headings in order — a dotted ordinal under the nearest
// shallower heading before — names each by its id attribute when it has one,
// and ends each section on the line before the next heading of the same or a
// shallower level.
func buildOutline(hs []heading, total int) []section {
	type open struct {
		level    int
		ordinal  string
		children int
	}
	var stack []open
	top := 0
	out := make([]section, 0, len(hs))
	for _, h := range hs {
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		var ordinal string
		if len(stack) == 0 {
			top++
			ordinal = strconv.Itoa(top)
		} else {
			parent := &stack[len(stack)-1]
			parent.children++
			ordinal = parent.ordinal + "." + strconv.Itoa(parent.children)
		}
		stack = append(stack, open{level: h.level, ordinal: ordinal})
		id := ordinal
		if h.id != "" {
			id = h.id
		}
		out = append(out, section{ID: id, Level: h.level, Heading: h.text, Start: h.line, End: total})
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].Level <= out[i].Level {
				out[i].End = out[j].Start - 1
				break
			}
		}
	}
	return out
}

var (
	atxHeading = regexp.MustCompile(`^(#{1,4})[ \t]+(.*?)[ \t]*#*[ \t]*$`)
	fenceLine  = regexp.MustCompile("^ {0,3}(```|~~~)")
)

// markdownOutline outlines Markdown from its #–#### headings, skipping fenced code.
func markdownOutline(lines []string) []section {
	var hs []heading
	var fence string
	for i, l := range lines {
		if m := fenceLine.FindStringSubmatch(l); m != nil {
			switch {
			case fence == "":
				fence = m[1]
			case fence == m[1]:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := atxHeading.FindStringSubmatch(l); m != nil {
			hs = append(hs, heading{level: len(m[1]), text: m[2], line: i + 1})
		}
	}
	return buildOutline(hs, len(lines))
}

// section finds a section by id, or by its heading text when exactly one
// heading has it (ignoring case). A miss or an ambiguous heading is coded.
func (v *textView) section(key string) (section, error) {
	key = strings.TrimSpace(key)
	for _, s := range v.outline {
		if s.ID == key {
			return s, nil
		}
	}
	var hits []section
	for _, s := range v.outline {
		if strings.EqualFold(s.Heading, key) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return section{}, coded("SECTION_NOT_FOUND", "no section "+strconv.Quote(key)+"; sections: "+sectionIDs(v.outline))
	default:
		return section{}, coded("SECTION_AMBIGUOUS", strconv.Itoa(len(hits))+" sections are headed "+strconv.Quote(key)+"; use one of: "+sectionIDs(hits))
	}
}

func sectionIDs(ss []section) string {
	if len(ss) == 0 {
		return "(none: this document has no headings; use offset and limit)"
	}
	ids := make([]string, len(ss))
	for i, s := range ss {
		ids[i] = s.ID
	}
	return strings.Join(ids, ", ")
}

// --- HTML ----------------------------------------------------------------------

// htmlBlocks start and end a line of the view.
var htmlBlocks = map[atom.Atom]bool{
	atom.Address: true, atom.Article: true, atom.Aside: true, atom.Blockquote: true, atom.Body: true,
	atom.Caption: true, atom.Dd: true, atom.Details: true, atom.Div: true, atom.Dl: true, atom.Dt: true,
	atom.Fieldset: true, atom.Figcaption: true, atom.Figure: true, atom.Footer: true, atom.Form: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Header: true, atom.Hr: true, atom.Html: true, atom.Li: true, atom.Main: true, atom.Nav: true,
	atom.Ol: true, atom.P: true, atom.Pre: true, atom.Section: true, atom.Summary: true, atom.Table: true,
	atom.Tbody: true, atom.Tfoot: true, atom.Thead: true, atom.Tr: true, atom.Ul: true,
}

// htmlSkipped are left out of the view with everything inside them.
var htmlSkipped = map[atom.Atom]bool{atom.Head: true, atom.Style: true, atom.Script: true, atom.Template: true}

func headingLevel(a atom.Atom) int {
	switch a {
	case atom.H1:
		return 1
	case atom.H2:
		return 2
	case atom.H3:
		return 3
	case atom.H4:
		return 4
	case atom.H5:
		return 5
	case atom.H6:
		return 6
	}
	return 0
}

// htmlRenderer turns a page's tokens into the view's lines.
type htmlRenderer struct {
	lines    []string
	cur      strings.Builder
	skip     []atom.Atom // open skipped elements, innermost last
	pre      int         // open pre elements
	preStart bool        // nothing of the open pre is out yet
	cells    int         // cells seen in the open row
	hLevel   int         // the open heading's level, 0 outside one
	hID      string
	headings []heading
	dts      []heading
	dtOpen   bool
}

// renderHTML builds the view of a page: head, style, script, template and
// comments dropped; one line per block; h1–h6 as "# Heading" … "###### Heading";
// table cells joined with " | "; pre lines kept as they are; entities decoded;
// whitespace collapsed elsewhere. The outline is h1–h4, or, on a page with
// fewer than two such headings, those headings and its dt terms at level 5.
func renderHTML(content string) *textView {
	r := &htmlRenderer{}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			r.start(tok, tt == html.SelfClosingTagToken)
		case html.EndTagToken:
			r.end(tok.DataAtom)
		case html.TextToken:
			r.text(tok.Data)
		}
	}
	r.flush()
	var hs []heading
	for _, h := range r.headings {
		if h.level <= 4 {
			hs = append(hs, h)
		}
	}
	if len(hs) < 2 && len(r.dts) > 0 {
		hs = mergeByLine(hs, r.dts)
	}
	return &textView{lines: r.lines, outline: buildOutline(hs, len(r.lines))}
}

func mergeByLine(a, b []heading) []heading {
	out := make([]heading, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if j == len(b) || (i < len(a) && a[i].line <= b[j].line) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	return out
}

func (r *htmlRenderer) start(tok html.Token, selfClosing bool) {
	a := tok.DataAtom
	if len(r.skip) > 0 {
		if htmlSkipped[a] && !selfClosing {
			r.skip = append(r.skip, a)
		}
		return
	}
	if htmlSkipped[a] {
		if !selfClosing {
			r.skip = append(r.skip, a)
		}
		return
	}
	switch {
	case a == atom.Br:
		r.flush()
		return
	case a == atom.Td || a == atom.Th:
		if r.cells > 0 {
			r.cur.WriteString(" | ")
		}
		r.cells++
		return
	}
	if !htmlBlocks[a] {
		return
	}
	r.flush()
	switch {
	case a == atom.Tr:
		r.cells = 0
	case a == atom.Pre && !selfClosing:
		r.pre++
		r.preStart = true
	case a == atom.Dt && !selfClosing:
		r.dtOpen = true
	case headingLevel(a) > 0 && !selfClosing:
		r.hLevel, r.hID = headingLevel(a), ""
		for _, at := range tok.Attr {
			if at.Key == "id" {
				r.hID = strings.TrimSpace(at.Val)
			}
		}
	}
}

func (r *htmlRenderer) end(a atom.Atom) {
	if len(r.skip) > 0 {
		if r.skip[len(r.skip)-1] == a {
			r.skip = r.skip[:len(r.skip)-1]
		}
		return
	}
	if a == atom.Br {
		r.flush()
		return
	}
	if !htmlBlocks[a] {
		return
	}
	r.flush()
	switch {
	case a == atom.Pre && r.pre > 0:
		r.pre--
	case a == atom.Dt:
		r.dtOpen = false
	case headingLevel(a) > 0:
		r.hLevel = 0
	case a == atom.Tr:
		r.cells = 0
	}
}

func (r *htmlRenderer) text(s string) {
	if len(r.skip) > 0 {
		return
	}
	if r.pre > 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		parts := strings.Split(s, "\n")
		for i, p := range parts {
			if i > 0 {
				r.flushPre()
			}
			r.cur.WriteString(p)
		}
		return
	}
	r.cur.WriteString(s)
}

// flushPre ends a line inside pre: kept as it is, blank lines too, except
// an empty first line (the newline right after <pre>).
func (r *htmlRenderer) flushPre() {
	line := strings.TrimRight(r.cur.String(), " \t\r")
	r.cur.Reset()
	if line == "" && r.preStart {
		r.preStart = false
		return
	}
	r.preStart = false
	r.lines = append(r.lines, line)
}

// flush ends the line being built, if it has any text.
func (r *htmlRenderer) flush() {
	raw := r.cur.String()
	r.cur.Reset()
	if r.pre > 0 {
		if line := strings.TrimRight(raw, " \t\r"); line != "" {
			r.lines = append(r.lines, line)
		}
		return
	}
	line := strings.Join(strings.Fields(raw), " ")
	if line == "" {
		return
	}
	if r.hLevel > 0 {
		r.headings = append(r.headings, heading{level: r.hLevel, id: r.hID, text: line, line: len(r.lines) + 1})
		line = strings.Repeat("#", r.hLevel) + " " + line
	} else if r.dtOpen {
		r.dts = append(r.dts, heading{level: 5, text: line, line: len(r.lines) + 1})
	}
	r.lines = append(r.lines, line)
}
