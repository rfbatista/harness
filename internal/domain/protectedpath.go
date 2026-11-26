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

// ShellWriteTargets are the files a shell command line redirects output to
// (>, >>, >|, &>, and a numbered descriptor such as 2>), as the words that
// follow the operators, with their quotes taken off. A descriptor duplication
// (2>&1), /dev/ files and anything inside quotes are not targets. It reads the
// command as written: a target made of a variable or a substitution is not
// resolved.
func ShellWriteTargets(command string) []string {
	var out []string
	words := shellWords(command)
	for i, w := range words {
		rest, ok := redirectOperator(w.text, w.quoted)
		if !ok {
			continue
		}
		target := rest
		if target == "" && i+1 < len(words) {
			target = words[i+1].text
		}
		if target == "" || strings.HasPrefix(target, "&") || strings.HasPrefix(target, "/dev/") || strings.HasPrefix(target, "$") || strings.HasPrefix(target, "(") {
			continue
		}
		out = append(out, target)
	}
	return out
}

// redirectOperator splits a word that starts with an output redirection into
// what is attached after the operator.
func redirectOperator(w string, quoted bool) (rest string, ok bool) {
	if quoted {
		return "", false
	}
	i := 0
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	if i < len(w) && w[i] == '&' && i+1 < len(w) && w[i+1] == '>' {
		i++
	}
	j := i
	for j < len(w) && w[j] == '>' {
		j++
	}
	if j == i || j-i > 2 {
		return "", false
	}
	if j < len(w) && w[j] == '|' {
		j++
	}
	return w[j:], true
}

type shellWord struct {
	text   string
	quoted bool
}

// shellWords splits a command line into words on blanks, newlines, ; and |
// outside quotes, and on & unless it belongs to a redirection. A word whose
// first character was quoted is marked quoted: it is text, not an operator.
func shellWords(s string) []shellWord {
	var words []shellWord
	var cur strings.Builder
	var quote rune
	leadQuoted, started := false, false
	flush := func() {
		if started {
			words = append(words, shellWord{text: cur.String(), quoted: leadQuoted})
		}
		cur.Reset()
		leadQuoted, started = false, false
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			if !started {
				leadQuoted = true
			}
			quote, started = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == ';':
			flush()
		case r == '|' && !(i > 0 && rs[i-1] == '>'):
			flush()
		case r == '&' && !(i+1 < len(rs) && rs[i+1] == '>') && !(i > 0 && rs[i-1] == '>'):
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return words
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
