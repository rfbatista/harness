package shell

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// FragmentHeader marks a response that is a page fragment, for the web client:
// it swaps only an answer that carries it, and falls back to a full
// navigation for anything else, so a full page is never swapped into #main.
const FragmentHeader = "X-Harness-Fragment"

// NavTarget is the id the web client names as the target of a boosted
// navigation (HX-Target). A swap region or a lazy section names its own id.
const NavTarget = "app-shell"

type fragmentKey struct{}

// WithFragment marks ctx as the request of a page fragment: the shell leaves
// the shared project frame out of the page (ADR 0064).
func WithFragment(ctx context.Context) context.Context {
	return context.WithValue(ctx, fragmentKey{}, true)
}

// IsFragment reports whether ctx belongs to a page fragment request.
func IsFragment(ctx context.Context) bool {
	v, _ := ctx.Value(fragmentKey{}).(bool)
	return v
}

// PageRequest reports whether r is a boosted click on a link between pages:
// htmx's HX-Request, boosted, targeting the app shell. A swap region or a lazy
// section names another target and is answered by its own handler.
func PageRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-Boosted") == "true" &&
		r.Header.Get("HX-Target") == NavTarget
}

// SameFrame reports whether a click from the page the browser shows (htmx's
// HX-Current-URL) to the page of r stays in one frame, so that only <main>
// swaps. Two pages of one project share a frame; a request with no current
// URL, or from another project or from outside any project, does not.
func SameFrame(r *http.Request) bool {
	to := frameOf(r.URL.Path)
	if to == "" {
		return false
	}
	current, err := url.Parse(r.Header.Get("HX-Current-URL"))
	if err != nil || current.Path == "" {
		return false
	}
	return frameOf(current.Path) == to
}

// frameOf is the project a path belongs to: its id, or "" for a page outside
// any project (the list, New project, the catalogs, the settings).
func frameOf(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "projects" || parts[1] == "new" {
		return ""
	}
	return parts[1]
}

// MarkFragment tells the client how to place the answer: a page fragment
// replaces #main, whatever target the click named.
func MarkFragment(h http.Header) {
	h.Set(FragmentHeader, "page")
	h.Set("HX-Retarget", "#main")
	h.Set("HX-Reswap", "outerHTML")
}

// VaryOnPage makes every answer of a page's URL vary on what picks its shape.
func VaryOnPage(h http.Header) {
	h.Add("Vary", "HX-Request")
	h.Add("Vary", "HX-Current-URL")
}
