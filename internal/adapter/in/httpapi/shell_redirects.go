package httpapi

import (
	"path/filepath"
	"strings"
)

// redirectTargets is every file a Bash command line redirects output into
// (>, >>, >|, &>, &>>, n>, >&file, <>), as absolute paths: relative ones
// are taken from cwd, following the line's cd commands, and $HARNESS_SHELL
// is read as shellDir. It reads the line the way sh does as far as quotes,
// escapes, comments, operators and here-documents go; other expansions are
// left as written, so such a target is not found inside the shell.
func redirectTargets(command, cwd, shellDir string) []string {
	l := &shLexer{s: command, shell: shellDir}
	l.run()
	var out []string
	start := true
	for i, t := range l.toks {
		switch t.kind {
		case shOp:
			start = true
		case shWord:
			if start && (t.text == "cd" || t.text == "pushd") && i+1 < len(l.toks) && l.toks[i+1].kind == shWord {
				cwd = absFrom(cwd, l.toks[i+1].text)
			}
			start = false
		case shRedirect:
			if t.text != "" {
				out = append(out, absFrom(cwd, t.text))
			}
		}
	}
	return out
}

func absFrom(cwd, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

type shKind int

const (
	shWord     shKind = iota
	shOp              // ; && || | & ( ) and the like: a new command starts
	shRedirect        // an output redirect; text is its target
)

type shTok struct {
	kind shKind
	text string
}

// shLexer splits a command line into words, operators and output
// redirects.
type shLexer struct {
	s     string
	i     int
	shell string
	toks  []shTok
	// heredocs are the delimiters of here-documents whose bodies start
	// after the current line, and whether their lines lose leading tabs.
	heredocs []heredoc
}

type heredoc struct {
	delim string
	tabs  bool
}

func (l *shLexer) rest() string { return l.s[l.i:] }

func (l *shLexer) op() { l.toks = append(l.toks, shTok{kind: shOp}) }

func (l *shLexer) run() {
	for l.i < len(l.s) {
		r := l.rest()
		switch {
		case r[0] == ' ' || r[0] == '\t':
			l.i++
		case r[0] == '\n':
			l.i++
			l.op()
			l.skipHeredocs()
		case strings.HasPrefix(r, "\\\n"):
			l.i += 2
		case r[0] == '#':
			for l.i < len(l.s) && l.s[l.i] != '\n' {
				l.i++
			}
		case strings.HasPrefix(r, "&>"):
			l.i += 2
			if strings.HasPrefix(l.rest(), ">") {
				l.i++
			}
			l.target()
		case strings.HasPrefix(r, "$("), strings.HasPrefix(r, "<("), strings.HasPrefix(r, ">("):
			l.i += 2
			l.op()
		case strings.ContainsRune(";&|()`", rune(r[0])):
			l.i++
			if len(r) > 1 && (r[:2] == "&&" || r[:2] == "||" || r[:2] == ";;" || r[:2] == "|&") {
				l.i++
			}
			l.op()
		case r[0] == '>':
			l.output()
		case r[0] == '<':
			l.input()
		default:
			w, quoted := l.word()
			if !quoted && isDigits(w) && l.i < len(l.s) && (l.s[l.i] == '>' || l.s[l.i] == '<') {
				continue // a file descriptor: the redirect follows
			}
			l.toks = append(l.toks, shTok{kind: shWord, text: w})
		}
	}
}

// output reads >, >>, >| or >&, at l.i, and its target.
func (l *shLexer) output() {
	l.i++
	r := l.rest()
	switch {
	case strings.HasPrefix(r, ">"), strings.HasPrefix(r, "|"):
		l.i++
	case strings.HasPrefix(r, "&"):
		l.i++
		if r = l.rest(); r != "" && (r[0] == '-' || isDigits(r[:1])) {
			for l.i < len(l.s) && (l.s[l.i] == '-' || isDigits(l.s[l.i:l.i+1])) {
				l.i++
			}
			return // a descriptor duplicated, not a file
		}
	}
	l.target()
}

// input reads <, <<, <<-, <<<, <& or <> at l.i, and its operand: <> opens
// its target for writing too, a here-document's body is skipped.
func (l *shLexer) input() {
	r := l.rest()
	switch {
	case strings.HasPrefix(r, "<<<"):
		l.i += 3
		l.skipSpace()
		l.word()
	case strings.HasPrefix(r, "<<"):
		l.i += 2
		tabs := strings.HasPrefix(l.rest(), "-")
		if tabs {
			l.i++
		}
		l.skipSpace()
		delim, _ := l.word()
		l.heredocs = append(l.heredocs, heredoc{delim: delim, tabs: tabs})
	case strings.HasPrefix(r, "<>"):
		l.i += 2
		l.target()
	default:
		l.i++
		if strings.HasPrefix(l.rest(), "&") {
			l.i++
		}
		l.skipSpace()
		l.word()
	}
}

func (l *shLexer) target() {
	l.skipSpace()
	w, _ := l.word()
	l.toks = append(l.toks, shTok{kind: shRedirect, text: w})
}

func (l *shLexer) skipSpace() {
	for l.i < len(l.s) && (l.s[l.i] == ' ' || l.s[l.i] == '\t') {
		l.i++
	}
}

// skipHeredocs skips the bodies of the here-documents the line just ended
// opened.
func (l *shLexer) skipHeredocs() {
	for _, h := range l.heredocs {
		for l.i < len(l.s) {
			end := strings.IndexByte(l.rest(), '\n')
			line := l.rest()
			if end >= 0 {
				line = line[:end]
				l.i += end + 1
			} else {
				l.i = len(l.s)
			}
			if h.tabs {
				line = strings.TrimLeft(line, "\t")
			}
			if line == h.delim {
				break
			}
		}
	}
	l.heredocs = nil
}

// word reads one word at l.i with its quotes removed and $HARNESS_SHELL
// expanded outside single quotes; quoted says some of it was quoted.
func (l *shLexer) word() (string, bool) {
	var b strings.Builder
	quoted := false
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || strings.IndexByte(";&|()<>`", c) >= 0:
			return b.String(), quoted
		case c == '\\':
			if l.i+1 < len(l.s) {
				b.WriteByte(l.s[l.i+1])
			}
			l.i += 2
		case c == '\'':
			quoted = true
			end := strings.IndexByte(l.s[l.i+1:], '\'')
			if end < 0 {
				b.WriteString(l.s[l.i+1:])
				l.i = len(l.s)
				continue
			}
			b.WriteString(l.s[l.i+1 : l.i+1+end])
			l.i += end + 2
		case c == '"':
			quoted = true
			l.i++
			for l.i < len(l.s) && l.s[l.i] != '"' {
				switch {
				case l.s[l.i] == '\\' && l.i+1 < len(l.s) && strings.IndexByte("\"\\$`\n", l.s[l.i+1]) >= 0:
					b.WriteByte(l.s[l.i+1])
					l.i += 2
				case l.s[l.i] == '$' && l.variable(&b):
				default:
					b.WriteByte(l.s[l.i])
					l.i++
				}
			}
			l.i++
		case c == '$' && strings.HasPrefix(l.rest(), "$("):
			return b.String(), quoted
		case c == '$' && l.variable(&b):
		default:
			b.WriteByte(c)
			l.i++
		}
	}
	return b.String(), quoted
}

// variable expands $HARNESS_SHELL or ${HARNESS_SHELL} at l.i into b.
func (l *shLexer) variable(b *strings.Builder) bool {
	const name = "HARNESS_SHELL"
	r := l.rest()
	var n int
	switch {
	case strings.HasPrefix(r, "${"+name+"}"):
		n = len(name) + 3
	case strings.HasPrefix(r, "$"+name) && (len(r) == len(name)+1 || !isNameByte(r[len(name)+1])):
		n = len(name) + 1
	default:
		return false
	}
	b.WriteString(l.shell)
	l.i += n
	return true
}

func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
