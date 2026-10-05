package main

import (
	"strings"
	"testing"
)

// The committed CSS must match tokens.json; regenerate with
// `go run ./cmd/designtokens` when this fails.
func TestGeneratedFilesAreFresh(t *testing.T) {
	if err := run("../../design-system", true); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateKeepsSourceOrderAndThemes(t *testing.T) {
	tokens, err := parse([]byte(`{
		"font": {"sans": "x"}, "text": {"b": "2", "a": "1"}, "leading": {}, "weight": {},
		"space": {"s": "8px"}, "size": {}, "radius": {}, "border": {}, "ease": {},
		"duration": {}, "z": {},
		"themes": {
			"light": {"color": {"ink": "black", "canvas": "white"}},
			"dark": {"color": {"ink": "white", "canvas": "black"}}
		},
		"utilities": [{"prefix": "color", "property": "color", "group": "color", "only": ["ink"]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := generate(tokens)
	if err != nil {
		t.Fatal(err)
	}
	vars, utils := out[0].content, out[1].content

	if strings.Index(vars, "--text-b") > strings.Index(vars, "--text-a") {
		t.Error("token order does not follow tokens.json")
	}
	if !strings.Contains(vars, `:root[data-theme="dark"] {`+"\n    --color-ink: white;") {
		t.Error("dark theme is not pinned by data-theme")
	}
	if !strings.Contains(utils, ".color-ink { color: var(--color-ink); }") || strings.Contains(utils, ".color-canvas") {
		t.Errorf("utilities ignore `only`:\n%s", utils)
	}
}

func TestUnknownUtilityTokenFails(t *testing.T) {
	tokens, _ := parse([]byte(`{"themes": {"light": {"color": {}}, "dark": {"color": {}}},
		"utilities": [{"prefix": "bg", "property": "background", "group": "color", "only": ["nope"]}]}`))
	if _, err := utilitiesCSS(tokens); err == nil {
		t.Fatal("want an error for a utility naming a missing token")
	}
}
