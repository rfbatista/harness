package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentFormatValid(t *testing.T) {
	for _, f := range []DocumentFormat{DocumentFormatMarkdown, DocumentFormatHTML, "text"} {
		if !f.Valid() {
			t.Errorf("%q should be valid", f)
		}
	}
	for _, f := range []DocumentFormat{"", "pdf", "HTML", "Markdown", "Text", "txt"} {
		if f.Valid() {
			t.Errorf("%q should not be valid", f)
		}
	}
}

func TestDocumentJSONCarriesFormat(t *testing.T) {
	b, err := json.Marshal(Document{ID: "d1", Format: DocumentFormatHTML})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"format":"html"`) {
		t.Fatalf("format missing from the wire: %s", b)
	}
}

// A plain-text document keeps its format as "text" on the wire and back.
func TestDocumentTextFormatRoundTripsUnchanged(t *testing.T) {
	in := Document{ID: "d1", Format: DocumentFormatText, Content: "line one\n  <b>not markup</b>\n"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"format":"text"`) {
		t.Fatalf("format missing from the wire: %s", b)
	}
	var out Document
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Format != DocumentFormatText || out.Content != in.Content {
		t.Fatalf("round trip changed the document: %+v", out)
	}
}

func TestDocumentScope_Valid(t *testing.T) {
	for _, s := range []DocumentScope{DocumentScopeTask, DocumentScopeProject} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []DocumentScope{"", "global", "Task"} {
		if s.Valid() {
			t.Errorf("%q should not be valid", s)
		}
	}
}

// Every JSON shape the server returns carries the scope, as the contracts say.
func TestDocument_ScopeIsOnTheWire(t *testing.T) {
	b, err := json.Marshal(Document{ID: "d1", Scope: DocumentScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"scope":"project"`) {
		t.Fatalf("scope missing: %s", b)
	}
}

// The version is the content's name: the same bytes give the same version,
// any change gives another, and it is 12 hex characters of sha256.
func TestDocumentVersion(t *testing.T) {
	// sha256("") = e3b0c44298fc1c149afbf4c8996fb924…
	if got := DocumentVersion(""); got != "e3b0c44298fc" {
		t.Fatalf("DocumentVersion(\"\") = %q", got)
	}
	a, b := DocumentVersion("<p>a</p>"), DocumentVersion("<p>b</p>")
	if a == b || len(a) != 12 || a != DocumentVersion("<p>a</p>") {
		t.Fatalf("versions %q %q", a, b)
	}
}
