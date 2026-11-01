package designsystem

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The parsers here are the inverse of the generators in files.go: what Files
// writes, they read back, and they also read the hand-written forms of a
// repository (a plain tokens.json, a DESIGN.md without the version line). They
// never fail on one bad token: that token is skipped with its reason, so a
// preview can say what was left out.

type skip = ports.DesignImportSkip

// ParseDesignMD reads a DESIGN.md: the frontmatter's name and description, one
// token per `name: value` pair of each group, and the body (after the version
// line) as the principles. Entries that are not tokens come back skipped.
func ParseDesignMD(data []byte) (id Identity, tokens []domain.DesignToken, principles string, skipped []skip, err error) {
	front, body := splitFrontmatter(string(data))
	principles = strings.TrimLeft(stripVersionLine(body), "\r\n")
	if front == "" {
		return id, nil, principles, nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return id, nil, "", nil, fmt.Errorf("the frontmatter is not YAML: %s", firstLine(err.Error()))
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return id, nil, "", nil, errors.New("the frontmatter is not a mapping")
	}
	top := doc.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		key, val := top.Content[i].Value, top.Content[i+1]
		switch {
		case key == "name" && val.Kind == yaml.ScalarNode:
			id.Name = val.Value
		case key == "description" && val.Kind == yaml.ScalarNode:
			id.Description = val.Value
		case key == "components":
			skipped = append(skipped, skip{Path: key, Reason: "component specs reference tokens and are not tokens; components import from components/*.html"})
		case val.Kind != yaml.MappingNode:
			skipped = append(skipped, skip{Path: key, Reason: "not a token group"})
		default:
			t, s := designMDGroup(key, val)
			tokens, skipped = append(tokens, t...), append(skipped, s...)
		}
	}
	return id, tokens, principles, skipped, nil
}

func designMDGroup(group string, node *yaml.Node) (tokens []domain.DesignToken, skipped []skip) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, val := node.Content[i].Value, node.Content[i+1]
		path := group + "." + name
		switch val.Kind {
		case yaml.ScalarNode:
			tokens, skipped = addToken(tokens, skipped, path, domain.DesignToken{Group: group, Name: name, Value: val.Value}, "")
		case yaml.MappingNode:
			parts := map[string]any{}
			for j := 0; j+1 < len(val.Content); j += 2 {
				if val.Content[j+1].Kind != yaml.ScalarNode {
					skipped = append(skipped, skip{Path: path + "." + val.Content[j].Value, Reason: "a composite value holds only plain values"})
					continue
				}
				parts[val.Content[j].Value] = val.Content[j+1].Value
			}
			tokens, skipped = addToken(tokens, skipped, path, domain.DesignToken{Group: group, Name: name, Value: parts}, "")
		default:
			skipped = append(skipped, skip{Path: path, Reason: "not a token value"})
		}
	}
	return tokens, skipped
}

// ParseTokensJSON reads a tokens.json: the W3C Design Tokens form the export
// writes, or the plain form `{ "<group>": { "<name>": "<value>" } }`, with the
// type inferred. Keys starting with `$` (but the generated $version and
// $date) and sections that hold no token are skipped.
func ParseTokensJSON(data []byte) (tokens []domain.DesignToken, skipped []skip, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	root, err := orderedObject(dec)
	if err != nil {
		return nil, nil, fmt.Errorf("not JSON: %s", firstLine(err.Error()))
	}
	for _, e := range root {
		switch {
		case e.key == "$version" || e.key == "$date":
		case strings.HasPrefix(e.key, "$"):
			skipped = append(skipped, skip{Path: e.key, Reason: "not a token section"})
		default:
			obj, ok := e.value.(orderedMap)
			if !ok {
				skipped = append(skipped, skip{Path: e.key, Reason: "not a token group"})
				continue
			}
			t, s := tokensJSONGroup(e.key, obj)
			tokens, skipped = append(tokens, t...), append(skipped, s...)
		}
	}
	return tokens, skipped, nil
}

func tokensJSONGroup(group string, obj orderedMap) (tokens []domain.DesignToken, skipped []skip) {
	groupType := ""
	for _, e := range obj {
		if e.key == "$type" {
			groupType, _ = e.value.(string)
		}
	}
	for _, e := range obj {
		if strings.HasPrefix(e.key, "$") {
			continue
		}
		path := group + "." + e.key
		switch v := e.value.(type) {
		case string, json.Number:
			tokens, skipped = addToken(tokens, skipped, path, domain.DesignToken{Group: group, Name: e.key, Value: scalarString(v)}, "")
		case orderedMap:
			t := domain.DesignToken{Group: group, Name: e.key, Type: groupType}
			hasValue := false
			for _, f := range v {
				switch f.key {
				case "$value":
					hasValue = true
					t.Value = tokenValue(f.value)
				case "$type":
					t.Type, _ = f.value.(string)
				case "$description":
					t.Note, _ = f.value.(string)
				}
			}
			if !hasValue {
				skipped = append(skipped, skip{Path: path, Reason: "not a token (no $value)"})
				continue
			}
			tokens, skipped = addToken(tokens, skipped, path, t, t.Type)
		default:
			skipped = append(skipped, skip{Path: path, Reason: "not a token value"})
		}
	}
	return tokens, skipped
}

// tokenValue is a $value as the record holds it: a string, or a flat object
// of strings.
func tokenValue(v any) any {
	if m, ok := v.(orderedMap); ok {
		parts := map[string]any{}
		for _, e := range m {
			parts[e.key] = scalarString(e.value)
		}
		return parts
	}
	return scalarString(v)
}

func scalarString(v any) any {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	}
	return fmt.Sprint(v)
}

// addToken appends t when it fits the record: its type is the given one, else
// inferred, and it passes the record's own validation. Otherwise the token is
// skipped with the reason.
func addToken(tokens []domain.DesignToken, skipped []skip, path string, t domain.DesignToken, declared string) ([]domain.DesignToken, []skip) {
	t.Type = declared
	if t.Type == "" {
		t.Type = inferType(t.Group, t.Value)
	}
	if t.Type == "" {
		return tokens, append(skipped, skip{Path: path, Reason: fmt.Sprintf("no token type fits %v", t.Value)})
	}
	if problems := (domain.DesignSystemRecord{Tokens: []domain.DesignToken{t}}).Validate(domain.DesignSystemRecord{}); len(problems) > 0 {
		return tokens, append(skipped, skip{Path: path, Reason: problems[0].Message})
	}
	return append(tokens, t), skipped
}

var (
	colorFunc     = regexp.MustCompile(`(?i)^(oklch|oklab|rgba?|hsla?|hwb|lab|lch|color|color-mix|light-dark)\(`)
	colorHex      = regexp.MustCompile(`^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	dimensionExpr = regexp.MustCompile(`(?i)^-?[0-9]*\.?[0-9]+(px|rem|em|ch|ex|vw|vh|svw|svh|dvw|dvh|lvw|lvh|vmin|vmax|pt|cm|mm|in|%)$|^(min|max|clamp|calc)\(`)
)

// inferType is the token type a plain value most likely has, "" when none of
// the record's types fits.
func inferType(group string, value any) string {
	if parts, ok := value.(map[string]any); ok {
		if len(parts) == 0 {
			return ""
		}
		if strings.HasPrefix(group, "shadow") {
			return domain.TokenShadow
		}
		return domain.TokenTypography
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	switch {
	case colorHex.MatchString(s) || colorFunc.MatchString(s) || s == "transparent" || strings.EqualFold(s, "currentcolor"):
		return domain.TokenColor
	case group == "font" || group == "fonts" || group == "typeface":
		return domain.TokenFont
	case dimensionExpr.MatchString(s):
		return domain.TokenDimension
	case s == "0":
		return domain.TokenNumber
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return domain.TokenNumber
	}
	if durationRe.MatchString(s) {
		return domain.TokenDuration
	}
	return ""
}

var durationRe = regexp.MustCompile(`^-?[0-9]*\.?[0-9]+m?s$`)

// ParseComponentHTML reads a components/<slug>.html in the format the export
// writes: the slug in the design-system-component meta, the name in the h1,
// the purpose, the do and don't lists and one section per preview, whose
// style is the preview's CSS and the rest its HTML.
func ParseComponentHTML(data []byte) (domain.DesignComponent, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return domain.DesignComponent{}, fmt.Errorf("not HTML: %s", firstLine(err.Error()))
	}
	c := domain.DesignComponent{Do: []string{}, Dont: []string{}, Previews: []domain.DesignPreview{}}
	var title string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "meta" && attr(n, "name") == "design-system-component":
				c.Slug = attr(n, "content")
			case n.Data == "title" && title == "":
				title = strings.TrimSpace(text(n))
			case n.Data == "h1" && c.Name == "":
				c.Name = strings.TrimSpace(text(n))
			case hasAttr(n, "data-purpose"):
				c.Purpose = strings.TrimSpace(text(n))
				return
			case hasAttr(n, "data-do"):
				c.Do = listItems(n)
				return
			case hasAttr(n, "data-dont"):
				c.Dont = listItems(n)
				return
			case n.Data == "section" && hasAttr(n, "data-preview"):
				c.Previews = append(c.Previews, preview(n))
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	if c.Name == "" {
		c.Name = title
	}
	if c.Slug == "" {
		return domain.DesignComponent{}, errors.New(`no <meta name="design-system-component">`)
	}
	return c, nil
}

func preview(section *html.Node) domain.DesignPreview {
	p := domain.DesignPreview{Label: attr(section, "data-label"), Variant: attr(section, "data-variant"), State: attr(section, "data-state")}
	var markup, css strings.Builder
	for ch := section.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.Data == "style" {
			css.WriteString(text(ch))
			continue
		}
		_ = html.Render(&markup, ch)
	}
	p.HTML, p.CSS = strings.TrimSpace(markup.String()), strings.TrimSpace(css.String())
	return p
}

func listItems(list *html.Node) []string {
	items := []string{}
	for ch := list.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.Data == "li" {
			items = append(items, strings.TrimSpace(text(ch)))
		}
	}
	return items
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

// splitFrontmatter cuts a leading `---` block off; front is "" when there is
// none.
func splitFrontmatter(s string) (front, body string) {
	s = strings.TrimPrefix(s, "\uFEFF")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return "", s
	}
	if end := strings.Index(rest, "\n---"); end >= 0 {
		after := rest[end+len("\n---"):]
		if after == "" || after[0] == '\n' || after[0] == '\r' {
			return rest[:end], strings.TrimPrefix(strings.TrimPrefix(after, "\r"), "\n")
		}
	}
	return "", s
}

var versionLineRe = regexp.MustCompile(`^<!-- design system version \d+ · [0-9-]+ -->\r?\n?`)

func stripVersionLine(body string) string {
	trimmed := strings.TrimLeft(body, "\r\n")
	return versionLineRe.ReplaceAllString(trimmed, "")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// orderedMap is a JSON object whose keys keep the order of the file, so the
// tokens keep it too.
type orderedMap []orderedEntry

type orderedEntry struct {
	key   string
	value any
}

func orderedObject(dec *json.Decoder) (orderedMap, error) {
	dec.UseNumber()
	v, err := orderedValue(dec)
	if err != nil {
		return nil, err
	}
	m, ok := v.(orderedMap)
	if !ok {
		return nil, errors.New("the top level is not an object")
	}
	return m, nil
}

func orderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		m := orderedMap{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := orderedValue(dec)
			if err != nil {
				return nil, err
			}
			m = append(m, orderedEntry{key: k.(string), value: v})
		}
		_, err := dec.Token()
		return m, err
	default:
		var list []any
		for dec.More() {
			v, err := orderedValue(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token()
		return list, err
	}
}
