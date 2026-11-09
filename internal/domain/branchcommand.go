package domain

import "strings"

// GitInvocation is one git command found in a shell command line: its
// arguments (without "git") and the directory it runs in.
type GitInvocation struct {
	Args []string
	Dir  string
}

// GitInvocations finds the git commands in a shell command line, as the branch
// gate's hook sees what an agent typed. It reads quotes, separators (; && || | &
// newlines, and ( ) ` that start a command) and the prefixes that run a
// command (VAR=x, env, command, sudo, exec, time, nohup), follows `cd <dir>`,
// and looks inside `sh -c "…"`. A script or make is not read: the git shim
// catches what runs inside one.
func GitInvocations(command, dir string) []GitInvocation {
	var out []GitInvocation
	for _, words := range splitCommands(command) {
		words = skipPrefixes(words)
		if len(words) == 0 {
			continue
		}
		name := words[0]
		switch {
		case name == "cd" && len(words) > 1:
			dir = joinDir(dir, words[1])
		case name == "git" || strings.HasSuffix(name, "/git"):
			out = append(out, GitInvocation{Args: words[1:], Dir: dir})
		case isShell(name):
			for i, w := range words {
				if w == "-c" && i+1 < len(words) {
					out = append(out, GitInvocations(words[i+1], dir)...)
					break
				}
			}
		}
	}
	return out
}

func isShell(name string) bool {
	switch name[strings.LastIndex(name, "/")+1:] {
	case "sh", "bash", "zsh", "dash":
		return true
	}
	return false
}

func skipPrefixes(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case isAssignment(w):
		case w == "env" || w == "command" || w == "sudo" || w == "exec" || w == "time" || w == "nohup" || w == "builtin":
		default:
			return words
		}
		words = words[1:]
	}
	return words
}

func isAssignment(w string) bool {
	i := strings.Index(w, "=")
	if i <= 0 {
		return false
	}
	for _, r := range w[:i] {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// splitCommands splits a command line into commands of words.
func splitCommands(s string) [][]string {
	var cmds [][]string
	var words []string
	var cur strings.Builder
	has := false
	flushWord := func() {
		if has {
			words = append(words, cur.String())
			cur.Reset()
			has = false
		}
	}
	flushCmd := func() {
		flushWord()
		if len(words) > 0 {
			cmds = append(cmds, words)
			words = nil
		}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < len(rs):
			i++
			if rs[i] != '\n' {
				cur.WriteRune(rs[i])
				has = true
			}
		case r == '\'':
			has = true
			for i++; i < len(rs) && rs[i] != '\''; i++ {
				cur.WriteRune(rs[i])
			}
		case r == '"':
			has = true
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]) {
					i++
				}
				cur.WriteRune(rs[i])
			}
		case r == ' ' || r == '\t':
			flushWord()
		case r == '\n' || r == ';' || r == '&' || r == '|' || r == '(' || r == ')' || r == '`' || r == '{' || r == '}':
			flushCmd()
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			flushCmd()
			i++
		case r == '#' && !has && len(words) == 0:
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flushCmd()
	return cmds
}
