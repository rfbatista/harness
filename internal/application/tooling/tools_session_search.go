package tooling

import (
	"context"
	"regexp"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const (
	maxPatternChars = 512
	// excerptChars is how much of a long line a match on it shows, around the match.
	excerptChars = 200
)

// searchSource is one document or text asset a search reads.
type searchSource struct {
	source  string // task_document, project_document or artifact
	id      string
	title   string
	content string
	kind    textKind
}

// searchMatch is one hit as search_documents reports it.
type searchMatch struct {
	Source    string   `json:"source"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Line      int      `json:"line"`
	Text      string   `json:"text"`
	Before    []string `json:"before"`
	After     []string `json:"after"`
	SectionID string   `json:"section_id,omitempty"`
}

func searchDocumentsTool(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) domain.Tool {
	return domain.Tool{
		Name: "search_documents",
		Description: "Search this task's documents and the project's documents (and, with include_artifacts, their text design assets) for a pattern, line by line, without reading them whole. " +
			"Each match gives the document, the line number (valid for read_task_document / read_project_document offset), the line, " +
			"a few lines of context and its section. Literal text by default; regex for RE2. Use it before reading a large document.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["pattern"],"properties":{` +
			`"pattern":{"type":"string","description":"What to find: literal text, or an RE2 regular expression when regex is true. At most 512 characters."},` +
			`"regex":{"type":"boolean","description":"Treat pattern as an RE2 regular expression. Default false."},` +
			`"case_sensitive":{"type":"boolean","description":"Match case exactly. Default false."},` +
			`"scope":{"type":"string","enum":["task","project","all"],"description":"task: the documents linked to this task; project: the project's documents; all (default): both."},` +
			`"document_id":{"type":"string","description":"Search this one document only."},` +
			`"include_artifacts":{"type":"boolean","description":"Also search the text design assets read_artifact can see (at most 50 per search; skipped counts the rest)."},` +
			`"context":{"type":"integer","minimum":0,"maximum":5,"description":"Lines of context before and after each match, 0–5. Default 1."},` +
			`"max_matches":{"type":"integer","minimum":1,"maximum":200,"description":"Most matches to return, 1–200. Default 50."},` +
			`"view":{"type":"string","enum":["text","raw"],"description":"text (default): search the page as plain lines. raw: search the stored markup."}}}`),
		Source: "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			q, err := parseSearch(args)
			if err != nil {
				return nil, err
			}
			documentID := getString(args, "document_id", "")
			sources, err := documentSources(planningSvc, scope, q.scope, documentID)
			if err != nil {
				return nil, err
			}
			skipped := 0
			if getBool(args, "include_artifacts", false) && documentID == "" {
				assets, n, err := artifactSources(ctx, scope, art)
				if err != nil {
					return nil, err
				}
				sources, skipped = append(sources, assets...), n
			}
			return runSearch(q, sources, skipped), nil
		},
	}
}

// searchQuery is a search's arguments, checked.
type searchQuery struct {
	re         *regexp.Regexp
	scope      string
	context    int
	maxMatches int
	raw        bool
}

func parseSearch(args map[string]any) (searchQuery, error) {
	q := searchQuery{scope: getString(args, "scope", "all"), context: 1, maxMatches: 50}
	pattern := getString(args, "pattern", "")
	if pattern == "" {
		return q, coded("INVALID_INPUT", "pattern is required")
	}
	if utf8.RuneCountInString(pattern) > maxPatternChars {
		return q, coded("INVALID_INPUT", "pattern is longer than 512 characters")
	}
	if !getBool(args, "regex", false) {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !getBool(args, "case_sensitive", false) {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return q, coded("INVALID_PATTERN", err.Error()+"; the pattern is RE2 (no lookarounds or backreferences); or drop regex to search literal text")
	}
	q.re = re
	switch q.scope {
	case "task", "project", "all":
	default:
		return q, coded("INVALID_INPUT", "scope must be task, project or all")
	}
	if n, given, ok := wholeNumber(args, "context"); given {
		if !ok || n < 0 || n > 5 {
			return q, coded("INVALID_INPUT", "context must be a whole number from 0 to 5")
		}
		q.context = n
	}
	if n, given, ok := wholeNumber(args, "max_matches"); given {
		if !ok || n < 1 || n > 200 {
			return q, coded("INVALID_INPUT", "max_matches must be a whole number from 1 to 200")
		}
		q.maxMatches = n
	}
	switch getString(args, "view", "text") {
	case "text", "":
	case "raw":
		q.raw = true
	default:
		return q, coded("INVALID_INPUT", "view must be text or raw")
	}
	return q, nil
}

// documentSources is what a search reads: with document_id, that document if
// the session can see it; otherwise the task's linked documents and/or the
// project's documents, each once.
func documentSources(planningSvc ports.Planning, scope *taskScope, which, documentID string) ([]searchSource, error) {
	if documentID != "" {
		d, err := scope.document(planningSvc, documentID)
		if err != nil {
			if d, err = scope.projectDocument(planningSvc, documentID); err != nil {
				return nil, coded("DOCUMENT_NOT_ON_TASK", "document is neither linked to this session's task nor a project document of this project")
			}
		}
		return []searchSource{sourceOf(d)}, nil
	}
	var out []searchSource
	seen := map[string]bool{}
	add := func(docs []*domain.Document) {
		for _, d := range docs {
			if !seen[d.ID] {
				seen[d.ID] = true
				out = append(out, sourceOf(d))
			}
		}
	}
	if which != "project" {
		add(planningSvc.ListTicketDocuments(scope.ticket.ID))
	}
	if which != "task" {
		add(planningSvc.ListDocuments(scope.session.ProjectID, domain.DocumentScopeProject))
	}
	return out, nil
}

// sourceOf is a document as a search source; a project document is reported
// as one whether or not it is also linked to this task.
func sourceOf(d *domain.Document) searchSource {
	source := "task_document"
	if d.Scope == domain.DocumentScopeProject {
		source = "project_document"
	}
	return searchSource{source: source, id: d.ID, title: d.Title, content: d.Content, kind: documentKind(d)}
}

// runSearch matches q against every source in order, up to max matches and
// the result budget. skipped counts sources left out before the search.
func runSearch(q searchQuery, sources []searchSource, skipped int) map[string]any {
	matches := []searchMatch{}
	used, truncated := 0, false
	for _, src := range sources {
		if truncated {
			break
		}
		tv := newTextView(src.content, src.kind)
		view := tv
		if q.raw {
			view = rawView(src.content)
		}
		for i, line := range view.lines {
			loc := q.re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			if len(matches) == q.maxMatches {
				truncated = true
				break
			}
			m := searchMatch{Source: src.source, ID: src.id, Title: src.title, Line: i + 1,
				Text: matchText(line, loc), Before: contextLines(view.lines, i-q.context, i), After: contextLines(view.lines, i+1, i+1+q.context)}
			if !q.raw {
				m.SectionID = sectionAt(tv.outline, i+1)
			}
			size := len(m.Text)
			for _, l := range m.Before {
				size += len(l)
			}
			for _, l := range m.After {
				size += len(l)
			}
			if len(matches) > 0 && used+size > budgetBytes {
				truncated = true
				break
			}
			used += size
			matches = append(matches, m)
		}
	}
	return map[string]any{"matches": matches, "searched": len(sources), "skipped": skipped, "truncated": truncated}
}

// matchText is the matched line, or, for a line too long to show, an excerpt
// around the match marked with "…" where it was cut.
func matchText(line string, loc []int) string {
	if utf8.RuneCountInString(line) <= maxLineChars {
		return line
	}
	pad := excerptChars / 2
	start, end := loc[0], loc[1]
	if utf8.RuneCountInString(line[start:end]) > excerptChars {
		// A match longer than the excerpt shows its start only.
		end, pad = start, 0
		for n := 0; n < excerptChars; n++ {
			_, size := utf8.DecodeRuneInString(line[end:])
			end += size
		}
	}
	for n := 0; n < pad && start > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(line[:start])
		start -= size
	}
	for n := 0; n < pad && end < len(line); n++ {
		_, size := utf8.DecodeRuneInString(line[end:])
		end += size
	}
	out := line[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out += "…"
	}
	return out
}

func contextLines(lines []string, from, to int) []string {
	out := []string{}
	for i := max(from, 0); i < to && i < len(lines); i++ {
		out = append(out, cutLine(lines[i]))
	}
	return out
}

// sectionAt is the innermost section holding line n, or "".
func sectionAt(outline []section, n int) string {
	id, level := "", 0
	for _, s := range outline {
		if s.Start <= n && n <= s.End && s.Level >= level {
			id, level = s.ID, s.Level
		}
	}
	return id
}
