package domain

import (
	"path"
	"strings"
)

// Protected paths (Glossary "Protected path"): the globs of a gated session's
// kept configuration its writes may not touch (ADR 0012).

// MatchProtectedPath says whether rel, a path relative to the repository root,
// is covered by glob. A glob is matched segment by segment from the root: "*"
// and "?" stay within a segment, "**" matches any number of segments, and a
// glob that names a directory covers everything under it. A name with no slash
// is a root-only name ("check.sh" is the root's, "**/check.sh" is any); a
// leading slash means the root too.
func MatchProtectedPath(glob, rel string) bool {
	glob = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(glob), "/"), "/")
	if glob == "" {
		return false
	}
	pat, segs := strings.Split(glob, "/"), splitRel(rel)
	if len(segs) == 0 {
		return false
	}
	// A glob that matches a leading part of the path covers the rest of it.
	for n := 1; n <= len(segs); n++ {
		if matchSegments(pat, segs[:n]) {
			return true
		}
	}
	return false
}

func splitRel(rel string) []string {
	rel = path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "./")
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") || rel == ".." || strings.HasPrefix(rel, "/") {
		return nil
	}
	return strings.Split(rel, "/")
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for skip := 0; skip <= len(segs); skip++ {
			if matchSegments(pat[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], segs[1:])
}

// ProtectedPathMessage is what a refused write tells the agent: the code, the
// path and the glob that covers it.
func ProtectedPathMessage(rel, glob string) string {
	return "PROTECTED_PATH: " + rel + " matches the protected path " + glob + ", which a gated session may not change. " +
		"The person configures which paths are protected, and the task's architect can give a session scope over one: ask for that."
}

// ProtectedGlob answers the first of globs that covers rel.
func ProtectedGlob(globs []string, rel string) (string, bool) {
	for _, g := range globs {
		if MatchProtectedPath(g, rel) {
			return g, true
		}
	}
	return "", false
}

// ScopeGlobError is nil when every glob of a session's scope is well formed, and
// otherwise a coded error naming the first that is not.
func ScopeGlobError(scope []string) error {
	for _, g := range scope {
		if e := globError(g); e != nil {
			return &StructuredError{Code: "INVALID_GLOB", Message: "scope: " + e.Message}
		}
	}
	return nil
}
