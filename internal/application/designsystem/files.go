package designsystem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"operators-mcp/internal/domain"
)

// File is one file of a design-system version: Path is relative to the
// folder the files go in (the shell's project/design-system/, a repository's
// export folder), forward slashes.
type File struct {
	Path    string
	Content []byte
}

// Files are the files of one version of a project's design system, in a
// fixed order: README.md, DESIGN.md, tokens.json. It is the one generator
// of the shell view and of the export (which leaves README.md out), so the
// bytes are the same wherever they are written. The output depends only on
// the arguments. A design system with nothing in it (version 0, or a record
// with no tokens, principles or components) has only a README.md that says
// so.
func Files(project string, version int, date time.Time, record domain.DesignSystemRecord) []File {
	day := date.UTC().Format("2006-01-02")
	if version == 0 || isEmpty(record) {
		return []File{{Path: "README.md", Content: []byte(emptyReadme)}}
	}
	return []File{
		{Path: "README.md", Content: []byte(readme(version, day))},
		{Path: "DESIGN.md", Content: []byte(designMD(project, version, day, record))},
		{Path: "tokens.json", Content: tokensJSON(version, day, record)},
	}
}

func isEmpty(r domain.DesignSystemRecord) bool {
	return len(r.Tokens) == 0 && strings.TrimSpace(r.Principles) == "" && len(r.Components) == 0
}

func versionLine(version int, day string) string {
	return fmt.Sprintf("<!-- design system version %d · %s -->", version, day)
}

const emptyReadme = "# Project design system\n\nThis project has no design system yet.\n"

// readmeFiles are the lines of README.md's "What is here" list, one per
// generated file; the files added later add theirs.
var readmeFiles = []string{
	"- `tokens.json`: the tokens (colors, spacing, type, radii, shadows, durations) with their type and note, in the W3C Design Tokens format.",
	"- `DESIGN.md`: the same tokens as YAML frontmatter, then the principles: when and why to use what.",
}

func readme(version int, day string) string {
	return fmt.Sprintf(`# Project design system

This folder is the look of every UI in this project, kept by the person in the harness web UI. It is version %d, saved %s.

## What is here

%s

## How to use it

1. Read the principles in `+"`DESIGN.md`"+` before you build or change UI.
2. Use tokens, never raw values. A token `+"`<group>.<name>`"+` is the CSS custom property `+"`--<group>-<name>`"+`; a composite token (typography, shadow) adds its key in kebab case, as in `+"`--typography-headline-font-family`"+`.
3. Use this repository's own token build when it has one; its source is these tokens.

## Changing it

These files are read-only: a write, rm, mv or touch here is refused. The design system changes only in the harness web UI (Design system page), or through a design session the person starts there. If it lacks something, say so in a task comment or in your spec and the person decides.
`, version, day, strings.Join(readmeFiles, "\n"))
}

// tokenGroup is the tokens of one group, in record order.
type tokenGroup struct {
	name   string
	tokens []domain.DesignToken
}

// groups gathers the tokens by group, the groups in the order each first
// appears.
func groups(tokens []domain.DesignToken) []tokenGroup {
	var out []tokenGroup
	index := map[string]int{}
	for _, t := range tokens {
		i, ok := index[t.Group]
		if !ok {
			i = len(out)
			index[t.Group] = i
			out = append(out, tokenGroup{name: t.Group})
		}
		out[i].tokens = append(out[i].tokens, t)
	}
	return out
}

// members are a composite value's keys in a fixed (alphabetical) order, with
// their values; nil for a scalar value.
func members(v any) ([]string, map[string]any) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, m
}

// quote is s as a double-quoted scalar, valid in YAML and JSON alike.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// yamlKey is a mapping key, quoted when YAML could read it as something
// other than a string.
func yamlKey(k string) string {
	switch k {
	case "y", "n", "yes", "no", "on", "off", "true", "false", "null":
		return quote(k)
	}
	return k
}

func scalar(v any) string {
	if s, ok := v.(string); ok {
		return quote(s)
	}
	return quote(fmt.Sprint(v))
}

// description is the first paragraph of the principles on one line: the
// first run of lines that is not a heading.
func description(principles string) string {
	var para []string
	for _, line := range strings.Split(principles, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" && len(para) > 0:
			return strings.Join(para, " ")
		case line == "" || strings.HasPrefix(line, "#") && len(para) == 0:
			continue
		}
		para = append(para, line)
	}
	return strings.Join(para, " ")
}

func designMD(project string, version int, day string, r domain.DesignSystemRecord) string {
	var b strings.Builder
	b.WriteString("---\nname: " + quote(project) + "\ndescription: " + quote(description(r.Principles)) + "\n")
	for _, g := range groups(r.Tokens) {
		b.WriteString(yamlKey(g.name) + ":\n")
		for _, t := range g.tokens {
			keys, parts := members(t.Value)
			if keys == nil {
				b.WriteString("  " + yamlKey(t.Name) + ": " + scalar(t.Value) + "\n")
				continue
			}
			b.WriteString("  " + yamlKey(t.Name) + ":\n")
			for _, k := range keys {
				b.WriteString("    " + yamlKey(k) + ": " + scalar(parts[k]) + "\n")
			}
		}
	}
	b.WriteString("---\n" + versionLine(version, day) + "\n")
	if r.Principles != "" {
		b.WriteString("\n" + r.Principles)
	}
	return b.String()
}

func tokensJSON(version int, day string, r domain.DesignSystemRecord) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"$version\": %d,\n  \"$date\": %s", version, quote(day))
	for _, g := range groups(r.Tokens) {
		b.WriteString(",\n  " + quote(g.name) + ": {")
		for i, t := range g.tokens {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n    " + quote(t.Name) + ": {\n      \"$value\": ")
			if keys, parts := members(t.Value); keys != nil {
				b.WriteString("{")
				for j, k := range keys {
					if j > 0 {
						b.WriteString(",")
					}
					b.WriteString("\n        " + quote(k) + ": " + scalar(parts[k]))
				}
				b.WriteString("\n      }")
			} else {
				b.WriteString(scalar(t.Value))
			}
			b.WriteString(",\n      \"$type\": " + quote(t.Type) + ",\n      \"$description\": " + quote(t.Note) + "\n    }")
		}
		b.WriteString("\n  }")
	}
	b.WriteString("\n}\n")
	return []byte(b.String())
}
