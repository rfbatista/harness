package domain

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// DesignSystemRecord is a project's design system: its tokens, its principles
// (one Markdown document, sectioned by its `##` headings) and its components.
// It is the one shape of the web API's JSON and of every stored version
// (ADR 0054).
type DesignSystemRecord struct {
	Tokens     []DesignToken     `json:"tokens"`
	Principles string            `json:"principles"`
	Components []DesignComponent `json:"components"`
}

// DesignToken is one design token. Value is a string, except for typography
// and shadow tokens, whose value is a flat object of strings.
type DesignToken struct {
	Group string `json:"group"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
	Note  string `json:"note"`
}

// DesignComponent is a design-system component: what it is for, how to use
// it, and one preview per variant or state.
type DesignComponent struct {
	Slug     string          `json:"slug"`
	Name     string          `json:"name"`
	Purpose  string          `json:"purpose"`
	Do       []string        `json:"do"`
	Dont     []string        `json:"dont"`
	Previews []DesignPreview `json:"previews"`
}

// DesignPreview is a framework-neutral HTML and CSS fragment built only from
// tokens. Variant and State are free labels; Label is shown.
type DesignPreview struct {
	Label   string `json:"label"`
	Variant string `json:"variant"`
	State   string `json:"state"`
	HTML    string `json:"html"`
	CSS     string `json:"css"`
}

// Token types.
const (
	TokenColor      = "color"
	TokenDimension  = "dimension"
	TokenFont       = "font"
	TokenNumber     = "number"
	TokenDuration   = "duration"
	TokenShadow     = "shadow"
	TokenTypography = "typography"
)

// Sources of a design-system version.
const DesignSourceManual = "manual"

// DesignByPerson is who wrote a version the person saved.
const DesignByPerson = "person"

// Validation codes of a refused design-system record.
const (
	DesignDuplicateToken     = "DUPLICATE_TOKEN"
	DesignBadName            = "BAD_NAME"
	DesignBadValue           = "BAD_VALUE"
	DesignUnknownToken       = "UNKNOWN_TOKEN"
	DesignTokenInUse         = "TOKEN_IN_USE"
	DesignDuplicateComponent = "DUPLICATE_COMPONENT"
	DesignUnsafePreview      = "UNSAFE_PREVIEW"
)

// DesignFieldError is one problem of a refused record: a JSON pointer to the
// field, a code and a message.
type DesignFieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DesignSummary says what a version changed against the one before it, per
// section, as "+added ~changed -removed".
type DesignSummary struct {
	Tokens     string `json:"tokens"`
	Principles string `json:"principles"`
	Components string `json:"components"`
}

// DesignSystemVersion is one immutable version of a project's design system.
type DesignSystemVersion struct {
	ProjectID  string
	Version    int
	At         time.Time
	By         string // "person"
	Source     string // "manual"
	ProposalID string
	Message    string
	Summary    DesignSummary
	Record     DesignSystemRecord
}

// DesignSystem is a project's design system as it is now: the latest version,
// or the empty record (Exists false, version 0) when none was ever saved.
type DesignSystem struct {
	ProjectID string
	Exists    bool
	Version   int
	UpdatedAt time.Time
	UpdatedBy string
	Record    DesignSystemRecord
}

var (
	designNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	designKeyPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	durationPattern   = regexp.MustCompile(`^-?[0-9]*\.?[0-9]+m?s$`)
	// cssVarRef finds a var(--name) reference and captures the name.
	cssVarRef  = regexp.MustCompile(`var\(\s*(--[A-Za-z0-9_-]+)`)
	scriptTag  = regexp.MustCompile(`(?i)<\s*script`)
	urlCall    = regexp.MustCompile(`(?i)url\(\s*['"]?\s*(data:)?`)
	importRule = regexp.MustCompile(`(?i)@import`)
)

// Normalize makes the empty lists of a record decoded from JSON empty, not
// nil, so they answer as [].
func (r *DesignSystemRecord) Normalize() {
	if r.Tokens == nil {
		r.Tokens = []DesignToken{}
	}
	if r.Components == nil {
		r.Components = []DesignComponent{}
	}
	for i := range r.Components {
		c := &r.Components[i]
		if c.Do == nil {
			c.Do = []string{}
		}
		if c.Dont == nil {
			c.Dont = []string{}
		}
		if c.Previews == nil {
			c.Previews = []DesignPreview{}
		}
	}
}

// TokenProperty is the CSS custom property of a token, `--<group>-<name>`, or
// of one member of a composite (typography, shadow) token,
// `--<group>-<name>-<key>` with key in kebab case. part is "" for a scalar
// token. It is the one place the rule lives: the page's swatches, the
// preview documents and the generated files all use it.
func TokenProperty(group, name, part string) string {
	p := "--" + group + "-" + name
	if part == "" {
		return p
	}
	var kebab strings.Builder
	for i, r := range part {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				kebab.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		kebab.WriteRune(r)
	}
	return p + "-" + kebab.String()
}

// Properties are the custom properties the token defines.
func (t DesignToken) Properties() []string {
	parts, ok := t.Value.(map[string]any)
	if !ok {
		return []string{TokenProperty(t.Group, t.Name, "")}
	}
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, TokenProperty(t.Group, t.Name, k))
	}
	sort.Strings(keys)
	return keys
}

// Key is how the token is named in messages: group.name.
func (t DesignToken) Key() string { return t.Group + "." + t.Name }

// Validate lists what is wrong with the record; none means it can be saved.
// previous is the record the save replaces (empty for a first save): a
// preview that uses a token the previous record had and this one lacks is
// TOKEN_IN_USE, not UNKNOWN_TOKEN.
func (r DesignSystemRecord) Validate(previous DesignSystemRecord) []DesignFieldError {
	var errs []DesignFieldError
	add := func(path, code, format string, a ...any) {
		errs = append(errs, DesignFieldError{Path: path, Code: code, Message: fmt.Sprintf(format, a...)})
	}
	known := map[string]bool{}
	seen := map[string]bool{}
	for i, t := range r.Tokens {
		path := fmt.Sprintf("/tokens/%d", i)
		if !designNamePattern.MatchString(t.Group) {
			add(path+"/group", DesignBadName, "group %q must match ^[a-z][a-z0-9-]*$", t.Group)
		}
		if !designNamePattern.MatchString(t.Name) {
			add(path+"/name", DesignBadName, "name %q must match ^[a-z][a-z0-9-]*$", t.Name)
		}
		if seen[t.Key()] {
			add(path, DesignDuplicateToken, "token %s is defined more than once", t.Key())
		}
		seen[t.Key()] = true
		if msg := tokenValueProblem(t); msg != "" {
			add(path+designValuePath(t), DesignBadValue, "%s", msg)
		}
		for _, p := range t.Properties() {
			known[p] = true
		}
	}

	slugs := map[string]bool{}
	removed := map[string][]string{} // token key -> components that use it
	removedKeys := previous.propertyOwners()
	for i, c := range r.Components {
		path := fmt.Sprintf("/components/%d", i)
		if !designNamePattern.MatchString(c.Slug) {
			add(path+"/slug", DesignBadName, "slug %q must match ^[a-z][a-z0-9-]*$", c.Slug)
		}
		if slugs[c.Slug] {
			add(path+"/slug", DesignDuplicateComponent, "component %q is defined more than once", c.Slug)
		}
		slugs[c.Slug] = true
		for j, p := range c.Previews {
			pp := fmt.Sprintf("%s/previews/%d", path, j)
			for _, field := range []struct{ name, text string }{{"html", p.HTML}, {"css", p.CSS}} {
				if msg := unsafePreviewProblem(field.text); msg != "" {
					add(pp+"/"+field.name, DesignUnsafePreview, "%s %s", field.name, msg)
				}
			}
			for _, m := range cssVarRef.FindAllStringSubmatch(p.CSS, -1) {
				if known[m[1]] {
					continue
				}
				if owner, ok := removedKeys[m[1]]; ok {
					if !slices.Contains(removed[owner], c.Slug) {
						removed[owner] = append(removed[owner], c.Slug)
					}
					continue
				}
				add(pp+"/css", DesignUnknownToken, "%s is not a token", m[1])
			}
		}
	}
	owners := make([]string, 0, len(removed))
	for k := range removed {
		owners = append(owners, k)
	}
	sort.Strings(owners)
	for _, k := range owners {
		add("/tokens", DesignTokenInUse, "token %s is used by %s", k, strings.Join(removed[k], ", "))
	}
	return errs
}

// propertyOwners maps each custom property the record defines to its token's
// key.
func (r DesignSystemRecord) propertyOwners() map[string]string {
	out := map[string]string{}
	for _, t := range r.Tokens {
		for _, p := range t.Properties() {
			out[p] = t.Key()
		}
	}
	return out
}

func designValuePath(t DesignToken) string {
	switch t.Type {
	case TokenColor, TokenDimension, TokenFont, TokenNumber, TokenDuration, TokenShadow, TokenTypography:
		return "/value"
	}
	return "/type"
}

// tokenValueProblem is why the token's value does not fit its type; "" when
// it does.
func tokenValueProblem(t DesignToken) string {
	switch t.Type {
	case TokenColor, TokenDimension, TokenFont, TokenNumber, TokenDuration:
		s, ok := t.Value.(string)
		if !ok {
			return fmt.Sprintf("a %s token's value is a string", t.Type)
		}
		if msg := cssValueProblem(s); msg != "" {
			return msg
		}
		if t.Type == TokenNumber {
			if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
				return fmt.Sprintf("%q is not a number", s)
			}
		}
		if t.Type == TokenDuration && !durationPattern.MatchString(strings.TrimSpace(s)) {
			return fmt.Sprintf("%q is not a duration such as 150ms or 0.2s", s)
		}
		return ""
	case TokenShadow, TokenTypography:
		parts, ok := t.Value.(map[string]any)
		if !ok || len(parts) == 0 {
			return fmt.Sprintf("a %s token's value is an object of strings", t.Type)
		}
		for k, v := range parts {
			if !designKeyPattern.MatchString(k) {
				return fmt.Sprintf("key %q must be letters, digits and dashes, starting with a letter", k)
			}
			s, ok := v.(string)
			if !ok {
				return fmt.Sprintf("%s.%s is not a string", t.Type, k)
			}
			if msg := cssValueProblem(s); msg != "" {
				return fmt.Sprintf("%s.%s: %s", t.Type, k, msg)
			}
		}
		return ""
	}
	return fmt.Sprintf("type %q is not one of color, dimension, font, number, duration, shadow, typography", t.Type)
}

// cssValueProblem refuses a value that is empty or could end the :root rule
// the value is written into.
func cssValueProblem(s string) string {
	if strings.TrimSpace(s) == "" {
		return "the value is empty"
	}
	if strings.ContainsAny(s, "{};<>") {
		return "the value holds a character that cannot be part of a CSS value"
	}
	return ""
}

// hasEventAttribute says any start tag of the html has an attribute whose
// name starts with "on". The tokenizer reads quotes and tag ends as a browser
// does, so a ">" inside a value does not hide a later attribute.
func hasEventAttribute(s string) bool {
	z := html.NewTokenizer(strings.NewReader(s))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			for {
				key, _, more := z.TagAttr()
				if strings.HasPrefix(strings.ToLower(string(key)), "on") {
					return true
				}
				if !more {
					break
				}
			}
		}
	}
}

// unsafePreviewProblem is why a preview's html or css is refused; "" when it
// is fine.
func unsafePreviewProblem(s string) string {
	switch {
	case scriptTag.MatchString(s):
		return "holds a <script>"
	case hasEventAttribute(s):
		return "holds an on* attribute"
	case importRule.MatchString(s):
		return "holds an @import"
	}
	for _, m := range urlCall.FindAllStringSubmatch(s, -1) {
		if m[1] == "" {
			return "holds a url( that is not a data: url"
		}
	}
	return ""
}

// RenameToken answers the record with the token group.name renamed to
// newGroup.newName and every var(--old) of it in the component previews
// (composite members included) pointing at the new name. ok is false when the
// record has no such token. The result is not validated.
func (r DesignSystemRecord) RenameToken(group, name, newGroup, newName string) (DesignSystemRecord, bool) {
	out := r
	out.Tokens = append([]DesignToken(nil), r.Tokens...)
	rewrite := map[string]string{}
	found := false
	for i, t := range out.Tokens {
		if t.Group != group || t.Name != name {
			continue
		}
		found = true
		before := t.Properties()
		out.Tokens[i].Group, out.Tokens[i].Name = newGroup, newName
		after := out.Tokens[i].Properties()
		for j := range before {
			rewrite[before[j]] = after[j]
		}
	}
	if !found {
		return r, false
	}
	out.Components = make([]DesignComponent, len(r.Components))
	for i, c := range r.Components {
		c.Previews = append([]DesignPreview(nil), c.Previews...)
		for j, p := range c.Previews {
			c.Previews[j].HTML = rewriteVarRefs(p.HTML, rewrite)
			c.Previews[j].CSS = rewriteVarRefs(p.CSS, rewrite)
		}
		out.Components[i] = c
	}
	return out, true
}

func rewriteVarRefs(s string, rewrite map[string]string) string {
	return cssVarRef.ReplaceAllStringFunc(s, func(m string) string {
		name := cssVarRef.FindStringSubmatch(m)[1]
		if to, ok := rewrite[name]; ok {
			return strings.Replace(m, name, to, 1)
		}
		return m
	})
}

// SummarizeDesignChange says what record changes against previous: tokens by
// group and name, principles by `##` section, components by slug.
func SummarizeDesignChange(previous, record DesignSystemRecord) DesignSummary {
	tokens := func(r DesignSystemRecord) map[string]any {
		m := map[string]any{}
		for _, t := range r.Tokens {
			m[t.Key()] = t
		}
		return m
	}
	components := func(r DesignSystemRecord) map[string]any {
		m := map[string]any{}
		for _, c := range r.Components {
			m[c.Slug] = c
		}
		return m
	}
	return DesignSummary{
		Tokens:     countChanges(tokens(previous), tokens(record)),
		Principles: countChanges(principleSections(previous.Principles), principleSections(record.Principles)),
		Components: countChanges(components(previous), components(record)),
	}
}

// countChanges is "+added ~changed -removed" between two keyed sets.
func countChanges(before, after map[string]any) string {
	var added, changed, removed int
	for k, v := range after {
		old, ok := before[k]
		switch {
		case !ok:
			added++
		case !reflect.DeepEqual(old, v):
			changed++
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			removed++
		}
	}
	return fmt.Sprintf("+%d ~%d -%d", added, changed, removed)
}

// principleSections splits Markdown at its `##` headings: each section is
// keyed by its heading, the text before the first heading by "".
func principleSections(md string) map[string]any {
	out := map[string]any{}
	key, body := "", []string{}
	flush := func() {
		if text := strings.TrimSpace(strings.Join(body, "\n")); key != "" || text != "" {
			out[key] = text
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			key, body = strings.TrimSpace(h), nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}
