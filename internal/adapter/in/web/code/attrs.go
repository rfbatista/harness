package code

import (
	"slices"
	"strconv"

	"github.com/a-h/templ"
)

// Attribute helpers for the first paint: an attribute is left out rather
// than written empty when its state is off.

func on(cond bool, name string, value any) templ.Attributes {
	if !cond {
		return nil
	}
	return templ.Attributes{name: value}
}

func currentAttrs(current bool, value string) templ.Attributes {
	return on(current, "aria-current", value)
}

func disabledAttrs(why string) templ.Attributes {
	if why == "" {
		return nil
	}
	return templ.Attributes{"aria-disabled": "true", "title": why}
}

func selectedAttrs(selected bool) templ.Attributes { return on(selected, "selected", true) }
func stateAttrs(state string) templ.Attributes     { return on(state != "", "data-state", state) }
func collapsedAttrs(c bool) templ.Attributes       { return on(c, "data-collapsed", true) }
func noNewlineAttrs(n bool) templ.Attributes       { return on(n, "data-no-newline", true) }
func emptyAttrs(e bool) templ.Attributes           { return on(e, "data-empty", true) }
func kindAttrs(kind string) templ.Attributes       { return on(kind != "", "data-kind", kind) }

func tabindex(first bool) string {
	if first {
		return "0"
	}
	return "-1"
}

func treeAttrs(r TreeRow) templ.Attributes {
	a := templ.Attributes{}
	if r.Expanded != "" {
		a["aria-expanded"] = r.Expanded
	}
	if r.Selected {
		a["aria-selected"] = "true"
	}
	if r.Deleted {
		a["data-change"] = "deleted"
	}
	return a
}

func spreadAttrs(s MapSegment) templ.Attributes {
	a := templ.Attributes{"style": "--share:" + strconv.Itoa(s.Share)}
	if s.Kind != "" {
		a["data-kind"] = s.Kind
	} else {
		a["data-hue"] = s.Hue
	}
	return a
}

// sideAttrs: the map has no side; a session's Changes and Files have one.
func sideAttrs(v View) templ.Attributes {
	if v.View == ViewMap {
		return templ.Attributes{"data-side": "none"}
	}
	return nil
}

func sideLabel(view string) string {
	if view == ViewChanges {
		return "Changed files"
	}
	return "Files"
}

// refOptions is the ref select's options, the current one always among them.
func refOptions(refs []string, current string) []string {
	if current == "" || slices.Contains(refs, current) {
		return refs
	}
	return append([]string{current}, refs...)
}
