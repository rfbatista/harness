package designsystem

import (
	"reflect"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The parsers are the inverse of the generators: parse(generate(x)) == x.
func TestParsingTheGeneratedFilesGivesTheRecordBack(t *testing.T) {
	record := domain.DesignSystemRecord{
		Tokens: []domain.DesignToken{
			{Group: "colors", Name: "ink", Type: "color", Value: "oklch(0.22 0.01 190)"},
			{Group: "colors", Name: "signal", Type: "color", Value: "#0a7"},
			{Group: "space", Name: "m", Type: "dimension", Value: "0.75rem"},
			{Group: "weight", Name: "bold", Type: "number", Value: "700"},
			{Group: "duration", Name: "fast", Type: "duration", Value: "150ms"},
			{Group: "typography", Name: "body", Type: "typography", Value: map[string]any{"fontFamily": "Inter, sans-serif", "fontSize": "0.875rem"}},
		},
		Principles: "# Principles\n\n## Calm\n\nKeep it quiet.\n",
	}
	files := Files(Identity{Name: "App", Slug: "app", Description: "The look"}, 3, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), record)
	content := map[string][]byte{}
	for _, f := range files {
		content[f.Path] = f.Content
	}

	id, designTokens, principles, skipped, err := ParseDesignMD(content["DESIGN.md"])
	if err != nil || len(skipped) != 0 {
		t.Fatalf("DESIGN.md: %v, skipped %v", err, skipped)
	}
	if id.Name != "App" || id.Description != "The look" || principles != record.Principles {
		t.Errorf("identity %+v, principles %q", id, principles)
	}
	if got := withoutNotes(designTokens); !reflect.DeepEqual(got, withoutNotes(record.Tokens)) {
		t.Errorf("DESIGN.md tokens\n got %v\nwant %v", got, record.Tokens)
	}

	jsonTokens, skipped, err := ParseTokensJSON(content["tokens.json"])
	if err != nil || len(skipped) != 0 {
		t.Fatalf("tokens.json: %v, skipped %v", err, skipped)
	}
	if !reflect.DeepEqual(jsonTokens, record.Tokens) {
		t.Errorf("tokens.json tokens\n got %v\nwant %v", jsonTokens, record.Tokens)
	}
}

func withoutNotes(tokens []domain.DesignToken) []domain.DesignToken {
	out := make([]domain.DesignToken, len(tokens))
	copy(out, tokens)
	for i := range out {
		out[i].Note = ""
	}
	return out
}

func TestPlainTokensGetInferredTypesAndSectionsThatAreNotTokensAreSkipped(t *testing.T) {
	tokens, skipped, err := ParseTokensJSON([]byte(`{
  "$comment": "x",
  "font": {"sans": "Inter, sans-serif"},
  "text": {"md": "1rem", "2xl": "1.75rem"},
  "z": {"modal": "100"},
  "ease": {"out": "cubic-bezier(0, 0, 0.2, 1)"},
  "themes": {"dark": {"color": {"ink": "#fff"}}},
  "utilities": [{"prefix": "x"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, tok := range tokens {
		types[tok.Key()] = tok.Type
	}
	want := map[string]string{"font.sans": "font", "text.md": "dimension", "text.2xl": "dimension", "z.modal": "number", "ease.out": "easing"}
	if !reflect.DeepEqual(types, want) {
		t.Errorf("types = %v, want %v", types, want)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Path] = s.Reason
	}
	for _, path := range []string{"$comment", "themes.dark", "utilities"} {
		if reasons[path] == "" {
			t.Errorf("%s is not skipped with a reason: %v", path, reasons)
		}
	}
}

func TestComponentPageParsesBackToItsParts(t *testing.T) {
	page := `<!doctype html><html><head><meta charset="utf-8"><title>Button</title>
<meta name="design-system-component" content="button">
<style>:root{ --colors-ink: #000; }</style></head>
<body>
<!-- design system version 12 · 2026-10-31 -->
<h1>Button</h1>
<p data-purpose>Starts an action.</p>
<ul data-do><li>Use one primary</li><li>Label with a verb</li></ul>
<ul data-dont><li>Nest buttons</li></ul>
<section data-preview data-variant="primary" data-state="default" data-label="Primary / default">
  <style>.b{ color: var(--colors-ink); }</style>
  <button class="b">Go</button>
</section>
</body></html>`
	got, err := ParseComponentHTML([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DesignComponent{
		Slug: "button", Name: "Button", Purpose: "Starts an action.",
		Do: []string{"Use one primary", "Label with a verb"}, Dont: []string{"Nest buttons"},
		Previews: []domain.DesignPreview{{
			Label: "Primary / default", Variant: "primary", State: "default",
			HTML: `<button class="b">Go</button>`, CSS: ".b{ color: var(--colors-ink); }",
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if _, err := ParseComponentHTML([]byte("<html><body><h1>x</h1></body></html>")); err == nil {
		t.Error("a page with no component meta parsed")
	}
}
