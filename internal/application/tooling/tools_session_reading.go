package tooling

import (
	"math"

	"operators-mcp/internal/domain"
)

// readSchemaProps are the window and section arguments every read tool takes,
// as JSON-schema properties after the id.
const readSchemaProps = `"offset":{"type":"integer","minimum":1,"description":"First line to read, 1-based (relative to the section when section is given). Default 1."},` +
	`"limit":{"type":"integer","minimum":1,"maximum":1000,"description":"Most lines to return, 1–1000. Default 200; a result also stops at 32 KiB of text and gives next_offset."},` +
	`"section":{"type":"string","description":"A section to read: its section_id from the outline, or its exact heading text when only one heading has it. Text view only."},` +
	`"view":{"type":"string","enum":["text","raw"],"description":"text (default): the page as plain lines, markup dropped. raw: the stored content split on newlines, to copy exact markup for edit_*_document."}`

// readArgs is what a read asked for, checked.
type readArgs struct {
	windowed bool // offset, limit or section was given
	offset   int
	limit    int
	section  string
	raw      bool
}

func parseReadArgs(args map[string]any) (readArgs, error) {
	r := readArgs{offset: 1, limit: 200, section: getString(args, "section", "")}
	if n, given, ok := wholeNumber(args, "offset"); given {
		if !ok || n < 1 {
			return r, coded("INVALID_INPUT", "offset must be a whole number of 1 or more")
		}
		r.offset, r.windowed = n, true
	}
	if n, given, ok := wholeNumber(args, "limit"); given {
		if !ok || n < 1 || n > 1000 {
			return r, coded("INVALID_INPUT", "limit must be a whole number from 1 to 1000")
		}
		r.limit, r.windowed = n, true
	}
	switch view := getString(args, "view", "text"); view {
	case "text", "":
	case "raw":
		r.raw = true
	default:
		return r, coded("INVALID_INPUT", "view must be text or raw")
	}
	if r.section != "" {
		if r.raw {
			return r, coded("INVALID_INPUT", "section reads use the text view; drop view, or read the raw view with offset and limit")
		}
		r.windowed = true
	}
	return r, nil
}

// wholeNumber reads a whole-number argument: given says it is there at all,
// ok that it is a whole number (JSON numbers arrive as float64).
func wholeNumber(args map[string]any, key string) (n int, given, ok bool) {
	v, present := args[key]
	if !present || v == nil {
		return 0, false, false
	}
	switch x := v.(type) {
	case int:
		return x, true, true
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<31 {
			return int(x), true, true
		}
	}
	return 0, true, false
}

// guard reports the guard as results carry it.
var guard = map[string]int{"bytes": guardBytes, "lines": guardLines}

// overGuard says whether content is too big to hand back whole.
func overGuard(content string, tv *textView) bool {
	return len(content) > guardBytes || len(tv.lines) > guardLines
}

// readView answers a read of content under the window, section and guard
// rules. key names the item in the answer ("document" or "artifact"); whole
// is the item with its content, answered alone when nothing was asked for
// and the content is under the guard; meta is the item without content, for
// every other answer.
func readView(content string, tv *textView, args map[string]any, key string, whole, meta any) (any, error) {
	r, err := parseReadArgs(args)
	if err != nil {
		return nil, err
	}
	if !r.windowed && !r.raw && !overGuard(content, tv) {
		return map[string]any{key: whole}, nil
	}
	view := tv
	if r.raw {
		view = rawView(content)
	}
	out := map[string]any{key: meta, "total_lines": len(view.lines), "guard": guard, "budget_bytes": budgetBytes}
	first, last, base := r.offset, len(view.lines), 0
	if r.section != "" {
		s, err := view.section(r.section)
		if err != nil {
			return nil, err
		}
		base = s.Start - 1
		first, last = base+r.offset, s.End
	} else if !r.windowed && !r.raw {
		out["outline"] = view.outlineOrEmpty()
	}
	lines, next := view.window(first, last, r.limit, budgetBytes)
	out["lines"] = lines
	if next == 0 {
		out["next_offset"] = nil
	} else {
		out["next_offset"] = next - base
	}
	out["truncated"] = next != 0
	return out, nil
}

func (v *textView) outlineOrEmpty() []section {
	if v.outline == nil {
		return []section{}
	}
	return v.outline
}

// documentWhole is a document as a read under the guard answers it: as it
// always was, content included, plus its size and version.
type documentWhole struct {
	*domain.Document
	Bytes   int    `json:"bytes"`
	Lines   int    `json:"lines"`
	Version string `json:"version"`
}

// readDocument answers read_task_document and read_project_document.
func readDocument(d *domain.Document, args map[string]any) (any, error) {
	tv := newTextView(d.Content, documentKind(d))
	meta := summarize(d, tv)
	whole := &documentWhole{Document: d, Bytes: meta.Bytes, Lines: meta.Lines, Version: meta.Version}
	return readView(d.Content, tv, args, "document", whole, meta)
}
