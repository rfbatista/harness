package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentFormatValid(t *testing.T) {
	for _, f := range []DocumentFormat{DocumentFormatMarkdown, DocumentFormatHTML} {
		if !f.Valid() {
			t.Errorf("%q should be valid", f)
		}
	}
	for _, f := range []DocumentFormat{"", "pdf", "HTML", "Markdown"} {
		if f.Valid() {
			t.Errorf("%q should not be valid", f)
		}
	}
}

func TestIsHTMLDocument(t *testing.T) {
	page := "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Plan</title></head><body><h1>Plan</h1></body></html>"
	accepted := []string{
		page,
		"  \n" + page,                          // leading whitespace
		"\uFEFF" + page,                        // byte-order mark
		strings.ToUpper(page[:15]) + page[15:], // <!DOCTYPE HTML>
		"<html><body>no doctype</body></html>",
	}
	for _, c := range accepted {
		if !IsHTMLDocument(c) {
			t.Errorf("accepted content refused: %q", c)
		}
	}
	refused := []string{
		"",
		"# Plan\n\nstep one",
		"<h1>Plan</h1><p>a fragment</p>",
		"# Plan\n\n```html\n<html><body></body></html>\n```", // Markdown that quotes HTML
		"<!doctype html><p>no html or body element</p>",
		"<html><head><title>x</title></head></html>", // no body
	}
	for _, c := range refused {
		if IsHTMLDocument(c) {
			t.Errorf("non-HTML content accepted: %q", c)
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
