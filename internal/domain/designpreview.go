package domain

import (
	"fmt"
	"sort"
	"strings"
)

// PreviewCSP is the Content-Security-Policy of a preview document: no
// script, no network. It is half of the sandbox, the iframe's `sandbox=""`
// is the other; either alone is not enough.
const PreviewCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:"

// RootRule is the `:root` rule that defines every token as its custom
// property, in record order; the members of a composite token follow it in
// the order of their property names. The browser has the same rule in
// designsystem/domain/preview.js, and a fixed-case test holds the two equal.
func RootRule(tokens []DesignToken) string {
	var b strings.Builder
	b.WriteString(":root{")
	for _, t := range tokens {
		parts, ok := t.Value.(map[string]any)
		if !ok {
			b.WriteString(TokenProperty(t.Group, t.Name, "") + ":" + designText(t.Value) + ";")
			continue
		}
		type member struct{ prop, value string }
		members := make([]member, 0, len(parts))
		for k, v := range parts {
			members = append(members, member{TokenProperty(t.Group, t.Name, k), designText(v)})
		}
		sort.Slice(members, func(i, j int) bool { return members[i].prop < members[j].prop })
		for _, m := range members {
			b.WriteString(m.prop + ":" + m.value + ";")
		}
	}
	out := strings.TrimSuffix(b.String(), ";")
	return out + "}"
}

func designText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// PreviewDocument is the page a component preview renders as, for any
// record's tokens: the server's review, the export and the browser all build
// this same document, loaded through `srcdoc` into `<iframe sandbox="">`.
func PreviewDocument(tokens []DesignToken, p DesignPreview) string {
	return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="` + PreviewCSP + `">
<style>` + RootRule(tokens) + `</style>
<style>` + p.CSS + `</style></head><body>` + p.HTML + `</body></html>`
}
