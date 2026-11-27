package domain

import (
	"path"
	"strings"
)

// The files a Bash command line writes by redirection, read as a shell would
// split it (ADR 0012). It is a reading of the text, not a shell: a target made
// of a variable or a substitution is not resolved and is left out, and the
// PATH shims and the sandbox are the layers behind it.

type shellTokKind int

const (
	tokWord     shellTokKind = iota + 1 // a word; unresolved says it holds $ or ` or a leading ~
	tokOp                               // ; & && || | | newline ( )
	tokRedirOut                         // > >> >| &> &>>
	tokRedirDup                         // >& and <&: a descriptor copy, not a file
	tokRedirIn                          // < and the heredoc and here-string forms
)

type shellTok struct {
	kind       shellTokKind
	text       string
	unresolved bool
}

func isShellMeta(r rune) bool {
	switch r {
	case ' ', '\t', '\n', ';', '&', '|', '(', ')', '<', '>':
		return true
	}
	return false
}

// tokenizeShell splits command into words, control operators and redirection
// operators, honouring quotes, backslashes, $(…) and `…`, and skipping the bodies
// of heredocs.
func tokenizeShell(command string) []shellTok {
	rs := []rune(command)
	var toks []shellTok
	var pending []string // heredoc delimiters whose bodies follow the next newline
	var pendingDash []bool
	i := 0
	peek := func(k int) rune {
		if i+k < len(rs) {
			return rs[i+k]
		}
		return 0
	}
	readWord := func() (string, bool) {
		var b strings.Builder
		unresolved := false
		for i < len(rs) && !isShellMeta(rs[i]) {
			r := rs[i]
			switch {
			case r == '\\' && i+1 < len(rs):
				b.WriteRune(rs[i+1])
				i += 2
			case r == '\'':
				i++
				for i < len(rs) && rs[i] != '\'' {
					b.WriteRune(rs[i])
					i++
				}
				i++
			case r == '"':
				i++
				for i < len(rs) && rs[i] != '"' {
					if rs[i] == '\\' && i+1 < len(rs) {
						i++
					} else if rs[i] == '$' || rs[i] == '`' {
						unresolved = true
					}
					b.WriteRune(rs[i])
					i++
				}
				i++
			case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
				unresolved = true
				depth := 0
				for i < len(rs) {
					b.WriteRune(rs[i])
					if rs[i] == '(' {
						depth++
					} else if rs[i] == ')' {
						depth--
						if depth == 0 {
							i++
							break
						}
					}
					i++
				}
			case r == '`':
				unresolved = true
				b.WriteRune(r)
				i++
				for i < len(rs) && rs[i] != '`' {
					b.WriteRune(rs[i])
					i++
				}
				if i < len(rs) {
					b.WriteRune(rs[i])
					i++
				}
			default:
				if r == '$' || (r == '~' && b.Len() == 0) {
					unresolved = true
				}
				b.WriteRune(r)
				i++
			}
		}
		return b.String(), unresolved
	}
	skipBlanks := func() {
		for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
			i++
		}
	}
	for i < len(rs) {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t':
			i++
		case r == '\n':
			toks = append(toks, shellTok{kind: tokOp, text: "\n"})
			i++
			for k, delim := range pending { // skip each heredoc body
				for i < len(rs) {
					j := i
					for j < len(rs) && rs[j] != '\n' {
						j++
					}
					line := string(rs[i:j])
					if pendingDash[k] {
						line = strings.TrimLeft(line, "\t")
					}
					i = j
					if i < len(rs) {
						i++
					}
					if line == delim {
						break
					}
				}
			}
			pending, pendingDash = nil, nil
		case r == ';' || r == '(' || r == ')':
			toks = append(toks, shellTok{kind: tokOp, text: string(r)})
			i++
		case r == '&':
			switch peek(1) {
			case '>':
				i += 2
				if peek(0) == '>' {
					i++
				}
				toks = append(toks, shellTok{kind: tokRedirOut, text: "&>"})
			case '&':
				i += 2
				toks = append(toks, shellTok{kind: tokOp, text: "&&"})
			default:
				i++
				toks = append(toks, shellTok{kind: tokOp, text: "&"})
			}
		case r == '|':
			if peek(1) == '|' {
				i += 2
				toks = append(toks, shellTok{kind: tokOp, text: "||"})
			} else {
				i++
				if peek(0) == '&' {
					i++
				}
				toks = append(toks, shellTok{kind: tokOp, text: "|"})
			}
		case r == '>':
			i++
			switch peek(0) {
			case '&':
				i++
				toks = append(toks, shellTok{kind: tokRedirDup, text: ">&"})
			case '>':
				i++
				toks = append(toks, shellTok{kind: tokRedirOut, text: ">>"})
			case '|':
				i++
				toks = append(toks, shellTok{kind: tokRedirOut, text: ">|"})
			default:
				toks = append(toks, shellTok{kind: tokRedirOut, text: ">"})
			}
		case r == '<':
			i++
			switch {
			case peek(0) == '<' && peek(1) == '<': // here-string: its word is not a file
				i += 2
				toks = append(toks, shellTok{kind: tokRedirIn, text: "<<<"})
			case peek(0) == '<': // heredoc
				i++
				dash := false
				if peek(0) == '-' {
					dash = true
					i++
				}
				skipBlanks()
				delim, _ := readWord()
				pending, pendingDash = append(pending, delim), append(pendingDash, dash)
				toks = append(toks, shellTok{kind: tokRedirIn, text: "<<"})
			case peek(0) == '&':
				i++
				toks = append(toks, shellTok{kind: tokRedirDup, text: "<&"})
			default:
				if peek(0) == '>' { // <> opens a file for reading and writing
					i++
					toks = append(toks, shellTok{kind: tokRedirOut, text: "<>"})
				} else {
					toks = append(toks, shellTok{kind: tokRedirIn, text: "<"})
				}
			}
		default:
			word, unresolved := readWord()
			if word == "" {
				i++ // a lone quote pair or stray character
				continue
			}
			// A number glued to a redirection is its descriptor, not a word.
			if isDigits(word) && (peek(0) == '>' || peek(0) == '<') {
				continue
			}
			toks = append(toks, shellTok{kind: tokWord, text: word, unresolved: unresolved})
		}
	}
	return toks
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shellWrite is one redirection target, with the directory it is relative to.
type shellWrite struct {
	target string
	cwd    string // meaningful when known
	known  bool   // false after a cd the text cannot resolve
}

// scanShellWrites walks the command's tokens, following cd, pushd and popd (and
// the parentheses that scope them), and answers every output redirection.
func scanShellWrites(command, cwd string, track bool) []shellWrite {
	type dir struct {
		cwd   string
		known bool
	}
	cur := dir{cwd, track && cwd != "" || !track}
	var subshells []dir
	var dirstack []dir
	var out []shellWrite
	toks := tokenizeShell(command)
	cmdStart := true
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.kind {
		case tokOp:
			switch t.text {
			case "(":
				subshells = append(subshells, cur)
			case ")":
				if n := len(subshells); n > 0 {
					cur, subshells = subshells[n-1], subshells[:n-1]
				}
			}
			cmdStart = true
		case tokRedirDup, tokRedirIn:
			if i+1 < len(toks) && toks[i+1].kind == tokWord {
				i++
			}
		case tokRedirOut:
			if i+1 < len(toks) && toks[i+1].kind == tokWord {
				i++
				w := toks[i]
				if w.unresolved || strings.HasPrefix(w.text, "&") || strings.HasPrefix(w.text, "/dev/") {
					continue
				}
				out = append(out, shellWrite{target: w.text, cwd: cur.cwd, known: cur.known})
			}
		case tokWord:
			if cmdStart && (t.text == "{" || t.text == "!") {
				continue
			}
			if !cmdStart || !track {
				cmdStart = false
				continue
			}
			cmdStart = false
			switch t.text {
			case "cd", "pushd":
				arg, argOK := "", false
				for j := i + 1; j < len(toks) && toks[j].kind == tokWord; j++ {
					if strings.HasPrefix(toks[j].text, "-") && toks[j].text != "-" {
						continue // -P, -L, -e, --
					}
					if !toks[j].unresolved && toks[j].text != "-" {
						arg, argOK = toks[j].text, true
					}
					break
				}
				if t.text == "pushd" {
					dirstack = append(dirstack, cur)
				}
				switch {
				case !argOK:
					cur = dir{known: false}
				case path.IsAbs(arg):
					cur = dir{path.Clean(arg), true}
				case cur.known:
					cur = dir{path.Join(cur.cwd, arg), true}
				}
			case "popd":
				if n := len(dirstack); n > 0 {
					cur, dirstack = dirstack[n-1], dirstack[:n-1]
				} else {
					cur = dir{known: false}
				}
			}
		}
	}
	return out
}

// ShellWriteTargets are the files a shell command line redirects output to
// (>, >>, >|, &>, <>, and a numbered descriptor such as 2>), as the words that
// follow the operators, with their quotes taken off, whether the operator is
// spaced or glued to a word, a parenthesis or a heredoc delimiter. A descriptor
// copy (2>&1), /dev/ files, a heredoc's body and anything inside quotes are not
// targets. A target made of a variable or a substitution is left out.
func ShellWriteTargets(command string) []string {
	var out []string
	for _, w := range scanShellWrites(command, "", false) {
		out = append(out, w.target)
	}
	return out
}

// ShellWrites are the files a command writes with the directory it runs in:
// absolute paths, followed through the cd, pushd and popd of the command itself,
// and the targets whose directory the text cannot resolve (a cd to a variable, a
// substitution, ~ or -), which could be anywhere.
type ShellWrites struct {
	Paths      []string
	Unresolved []string
}

// ShellWritesFrom reads command as run in cwd (an absolute path).
func ShellWritesFrom(cwd, command string) ShellWrites {
	var out ShellWrites
	for _, w := range scanShellWrites(command, cwd, true) {
		switch {
		case path.IsAbs(w.target):
			out.Paths = append(out.Paths, path.Clean(w.target))
		case w.known:
			out.Paths = append(out.Paths, path.Join(w.cwd, w.target))
		default:
			out.Unresolved = append(out.Unresolved, w.target)
		}
	}
	return out
}
