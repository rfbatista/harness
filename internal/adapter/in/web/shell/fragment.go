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

// Mode is the shape a page's answer takes.
type Mode int

const (
	// ModeFull is the whole document.
	ModeFull Mode = iota
	// ModeFragment is <main> and the out-of-band items, for a click within a
	// frame: the shared project frame is not built (ADR 0064).
	ModeFragment
	// ModeShell is the whole app shell without a document around it, for a
	// click to another frame.
	ModeShell
)

type modeKey struct{}

// WithMode records the shape of the answer on ctx.
func WithMode(ctx context.Context, m Mode) context.Context {
	return context.WithValue(ctx, modeKey{}, m)
}

// ModeOf is the shape of the answer to the request ctx belongs to.
func ModeOf(ctx context.Context) Mode {
	m, _ := ctx.Value(modeKey{}).(Mode)
	return m
}

// IsFragment reports whether ctx belongs to a page fragment request.
func IsFragment(ctx context.Context) bool { return ModeOf(ctx) == ModeFragment }

// PageRequest reports whether r is a boosted click on a link between pages:
// htmx's HX-Request, boosted, targeting the app shell. A swap region or a lazy
// section names another target and is answered by its own handler.
func PageRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-Boosted") == "true" &&
		r.Header.Get("HX-Target") == NavTarget
}

// SectionTarget is the id of the lazy section that r asks for at its page's own
// URL (htmx's HX-Target), or "" for any other request. A page that has lazy
// sections answers it with that section alone (ADR 0064).
func SectionTarget(r *http.Request) string {
	if r.Header.Get("HX-Request") != "true" || PageRequest(r) {
		return ""
	}
	return r.Header.Get("HX-Target")
}

// SameFrame reports whether a click from the page the browser shows (htmx's
// HX-Current-URL) to the page of r stays in one frame, so that only <main>
// swaps. Two pages of one project share a frame, and so do two pages outside
// any project; a request with no current URL, or from another project, or in
// or out of a project, does not.
func SameFrame(r *http.Request) bool {
	to := frameOf(r.URL.Path)
	current, err := url.Parse(r.Header.Get("HX-Current-URL"))
	if err != nil || current.Path == "" {
		return false
	}
	return frameOf(current.Path) == to
}

// frameOf names the frame a path is served in: the project's id, or for a page
// outside any project (the list, New project, the catalogs, the settings) the
// one frame of such pages. The Agents pages draw no rail, so they are a frame
// of their own.
func frameOf(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case parts[0] == "agents":
		return "~agents"
	case len(parts) < 2 || parts[0] != "projects" || parts[1] == "new":
		return "~"
	}
	return parts[1]
}

// MarkAnswer tells the client how to place the answer: a page fragment
// replaces #main, whatever target the click named; an app shell replaces the
// target the click named, the app shell.
func MarkAnswer(h http.Header, m Mode) {
	switch m {
	case ModeFragment:
		h.Set(FragmentHeader, "page")
		h.Set("HX-Retarget", "#main")
		h.Set("HX-Reswap", "outerHTML")
	case ModeShell:
		h.Set(FragmentHeader, "shell")
	}
}

// PrefetchHeader marks a request that the browser made ahead of the click, on
// hover or focus, and the click that uses what it fetched.
const PrefetchHeader = "X-Harness-Prefetch"

// Prefetched reports whether r asks for a page ahead of a click (ADR 0064).
func Prefetched(r *http.Request) bool {
	return PageRequest(r) && r.Header.Get(PrefetchHeader) == "1"
}

// PrefetchHold is how long the browser may keep a prefetched page for the click
// that follows (ADR 0062: a held value is about 5 s old at most).
const PrefetchHold = "private, max-age=5"

// VaryOnPage makes every answer of a page's URL vary on what picks its shape,
// and on whether it is a prefetched one: only the click that says so uses it.
func VaryOnPage(h http.Header) {
	h.Add("Vary", "HX-Request")
	h.Add("Vary", "HX-Current-URL")
	h.Add("Vary", PrefetchHeader)
}
