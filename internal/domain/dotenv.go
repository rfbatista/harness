package domain

import "strings"

// EnvVar is one variable of an env file, as an app run's environment gets it.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ParseDotenv reads an env file's content as dotenv, never as shell: no
// interpolation and no command substitution, so `$(…)` stays text. A line is
// KEY=VALUE with an optional `export ` prefix; the value is single-quoted
// (literal), double-quoted (\n, \" and \\ escapes) or unquoted (trimmed, an
// inline ` #` comment dropped). Blank lines and full-line `#` comments are
// ignored. The variables come in file order, a key given twice keeping its
// first place and its last value. skipped holds the 1-based numbers of the
// lines that are none of these; it never says what they held.
func ParseDotenv(text string) (vars []EnvVar, skipped []int) {
	at := map[string]int{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := parseDotenvLine(line)
		if !ok {
			skipped = append(skipped, i+1)
			continue
		}
		if j, seen := at[key]; seen {
			vars[j].Value = value
			continue
		}
		at[key] = len(vars)
		vars = append(vars, EnvVar{Key: key, Value: value})
	}
	return vars, skipped
}

func parseDotenvLine(line string) (key, value string, ok bool) {
	if rest, found := strings.CutPrefix(line, "export"); found && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		line = strings.TrimSpace(rest)
	}
	key, raw, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || !ValidEnvKey(key) || strings.ContainsRune(raw, 0) {
		return "", "", false
	}
	v := strings.TrimLeft(raw, " \t")
	switch {
	case strings.HasPrefix(v, "'"):
		end := strings.IndexByte(v[1:], '\'')
		if end < 0 || !onlyComment(v[end+2:]) {
			return "", "", false
		}
		return key, v[1 : end+1], true
	case strings.HasPrefix(v, `"`):
		raw = v
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			switch c := raw[i]; {
			case c == '"':
				if !onlyComment(raw[i+1:]) {
					return "", "", false
				}
				return key, b.String(), true
			case c == '\\' && i+1 < len(raw):
				switch raw[i+1] {
				case 'n':
					b.WriteByte('\n')
					i++
				case '"', '\\':
					b.WriteByte(raw[i+1])
					i++
				default:
					b.WriteByte(c)
				}
			default:
				b.WriteByte(c)
			}
		}
		return "", "", false // never closed
	default:
		// A # opens a comment after whitespace, the one before the value too.
		for i := 1; i < len(raw); i++ {
			if raw[i] == '#' && (raw[i-1] == ' ' || raw[i-1] == '\t') {
				raw = raw[:i]
				break
			}
		}
		return key, strings.TrimSpace(raw), true
	}
}

// onlyComment reports whether what follows a closing quote is nothing or a
// comment.
func onlyComment(rest string) bool {
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

// ValidEnvKey reports whether k can name an environment variable a shell
// exports: [A-Za-z_][A-Za-z0-9_]*.
func ValidEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}
