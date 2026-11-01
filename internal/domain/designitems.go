package domain

import (
	"encoding/json"
	"slices"
	"strings"
)

// The kinds and operations of the items a proposal is made of, and the
// decision on each (contract "Harness server ↔ Web UI — project design
// system", "Design sessions and proposals").
const (
	ItemToken     = "token"
	ItemPrinciple = "principles_section"
	ItemComponent = "component"

	DesignOpAdd    = "add"
	DesignOpChange = "change"
	DesignOpRemove = "remove"

	DecisionPending  = "pending"
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
	DecisionSentBack = "sent_back"
)

// IntroSection is the key of the principles' text before the first `## `
// heading.
const IntroSection = "(intro)"

// DesignItem is one change a draft makes to the version it started from:
// what the person approves or rejects. Base, Current and Proposed hold the
// token, the section {heading, markdown} or the component, nil for a side
// that does not exist.
type DesignItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Op       string `json:"op"`
	Key      string `json:"key"`
	Base     any    `json:"base"`
	Current  any    `json:"current"`
	Proposed any    `json:"proposed"`
	// Conflicting is true when the design system's current value differs from
	// the base the draft started from.
	Conflicting bool   `json:"conflicting"`
	Decision    string `json:"decision"`
}

// PrincipleSection is a part of the principles: the text under a `## `
// heading, or the intro before the first one.
type PrincipleSection struct {
	Heading  string `json:"heading"`
	Markdown string `json:"markdown"`
}

// SplitPrinciples cuts the principles at their `## ` headings: the intro (the
// text before the first one, possibly empty) and one section per heading, each
// holding its heading line and what follows it up to the next. Joining them
// again gives the text back exactly.
func SplitPrinciples(text string) (intro string, sections []PrincipleSection) {
	lines := strings.SplitAfter(text, "\n")
	var cur *PrincipleSection
	var introB strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			sections = append(sections, PrincipleSection{Heading: strings.TrimSpace(line)})
			cur = &sections[len(sections)-1]
		}
		switch {
		case cur != nil:
			cur.Markdown += line
		default:
			introB.WriteString(line)
		}
	}
	return introB.String(), sections
}

// JoinPrinciples is SplitPrinciples' inverse.
func JoinPrinciples(intro string, sections []PrincipleSection) string {
	var b strings.Builder
	b.WriteString(intro)
	for _, s := range sections {
		b.WriteString(s.Markdown)
	}
	return b.String()
}

// itemValues is a record as the items see it: each value by item key.
type itemValues struct {
	tokens     map[string]DesignToken
	sections   map[string]PrincipleSection
	components map[string]DesignComponent
	// Orders keep the record's order for what is added.
	tokenKeys, sectionKeys, componentKeys []string
}

func valuesOf(r DesignSystemRecord) itemValues {
	v := itemValues{tokens: map[string]DesignToken{}, sections: map[string]PrincipleSection{}, components: map[string]DesignComponent{}}
	for _, t := range r.Tokens {
		v.tokens[t.Key()] = t
		v.tokenKeys = append(v.tokenKeys, t.Key())
	}
	intro, sections := SplitPrinciples(r.Principles)
	if strings.TrimSpace(intro) != "" {
		v.sections[IntroSection] = PrincipleSection{Heading: IntroSection, Markdown: intro}
		v.sectionKeys = append(v.sectionKeys, IntroSection)
	}
	for _, s := range sections {
		v.sections[s.Heading] = s
		v.sectionKeys = append(v.sectionKeys, s.Heading)
	}
	for _, c := range r.Components {
		v.components[c.Slug] = c
		v.componentKeys = append(v.componentKeys, c.Slug)
	}
	return v
}

// same compares two values by their JSON.
func same(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func itemID(kind, key string) string { return kind + ":" + key }

// DeriveDesignItems compares a draft with its base and lists the items: one
// per token (group, name), per principles section (the intro included) and per
// component slug that was added, changed or removed. A rename is a remove
// plus an add. current is the design system's record now: an item whose
// current value differs from its base is conflicting.
func DeriveDesignItems(base, draft, current DesignSystemRecord) []DesignItem {
	b, d, c := valuesOf(base), valuesOf(draft), valuesOf(current)
	var items []DesignItem
	add := func(kind, key string, bv, dv, cv any, hasB, hasD, hasC bool) {
		it := DesignItem{ID: itemID(kind, key), Kind: kind, Key: key, Decision: DecisionPending}
		switch {
		case hasB && hasD && same(bv, dv):
			return
		case !hasB && !hasD:
			return
		case !hasB:
			it.Op = DesignOpAdd
		case !hasD:
			it.Op = DesignOpRemove
		default:
			it.Op = DesignOpChange
		}
		if hasB {
			it.Base = bv
		}
		if hasD {
			it.Proposed = dv
		}
		if hasC {
			it.Current = cv
		}
		it.Conflicting = hasB != hasC || (hasB && !same(bv, cv))
		items = append(items, it)
	}
	for _, key := range orderedKeys(b.tokenKeys, d.tokenKeys) {
		bv, hasB := b.tokens[key]
		dv, hasD := d.tokens[key]
		cv, hasC := c.tokens[key]
		add(ItemToken, key, bv, dv, cv, hasB, hasD, hasC)
	}
	for _, key := range orderedKeys(b.sectionKeys, d.sectionKeys) {
		bv, hasB := b.sections[key]
		dv, hasD := d.sections[key]
		cv, hasC := c.sections[key]
		add(ItemPrinciple, key, bv, dv, cv, hasB, hasD, hasC)
	}
	for _, key := range orderedKeys(b.componentKeys, d.componentKeys) {
		bv, hasB := b.components[key]
		dv, hasD := d.components[key]
		cv, hasC := c.components[key]
		add(ItemComponent, key, bv, dv, cv, hasB, hasD, hasC)
	}
	return items
}

// orderedKeys is the draft's keys in its order, then the base's that the
// draft lacks.
func orderedKeys(base, draft []string) []string {
	out := slices.Clone(draft)
	for _, k := range base {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// ApplyDesignItems applies the items on top of record, the design system's
// current record: tokens, principles sections and components added, replaced
// or removed by key. Applied to the record the items were derived from, with
// every item, it gives the draft back.
func ApplyDesignItems(record DesignSystemRecord, items []DesignItem) DesignSystemRecord {
	out := DesignSystemRecord{Tokens: slices.Clone(record.Tokens), Components: slices.Clone(record.Components)}
	intro, sections := SplitPrinciples(record.Principles)
	for _, it := range items {
		switch it.Kind {
		case ItemToken:
			t, _ := it.Proposed.(DesignToken)
			out.Tokens = upsert(out.Tokens, it.Key, it.Op == DesignOpRemove, t, DesignToken.Key)
		case ItemComponent:
			c, _ := it.Proposed.(DesignComponent)
			out.Components = upsert(out.Components, it.Key, it.Op == DesignOpRemove, c, func(c DesignComponent) string { return c.Slug })
		case ItemPrinciple:
			s, _ := it.Proposed.(PrincipleSection)
			if it.Key == IntroSection {
				intro = s.Markdown
				if it.Op == DesignOpRemove {
					intro = ""
				}
				continue
			}
			sections = upsert(sections, it.Key, it.Op == DesignOpRemove, s, func(s PrincipleSection) string { return s.Heading })
		}
	}
	out.Principles = JoinPrinciples(intro, sections)
	out.Normalize()
	return out
}

// upsert replaces the element with the key, adds it at the end when there is
// none, or drops it when remove.
func upsert[T any](list []T, key string, remove bool, v T, keyOf func(T) string) []T {
	for i, have := range list {
		if keyOf(have) != key {
			continue
		}
		if remove {
			return slices.Delete(list, i, i+1)
		}
		list[i] = v
		return list
	}
	if remove {
		return list
	}
	return append(list, v)
}
