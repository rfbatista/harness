package tooling

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// lint runs lightweight syntax checks on content and returns warnings (empty if clean).
// When tree-sitter supports the file type, it parses the AST and reports any
// ERROR / MISSING nodes — the same approach Aider uses in linter.py.
func lint(path, content string) string {
	var warnings []string

	ext := filepath.Ext(path)

	if TSSupportsExt(ext) {
		if w := TSLint([]byte(content), path); w != "" {
			warnings = append(warnings, w)
		}
	} else {
		if w := CheckBracketBalance(content); w != "" {
			warnings = append(warnings, w)
		}
	}

	switch ext {
	case ".go":
		if w := checkGoBasics(path); w != "" {
			warnings = append(warnings, w)
		}
	case ".js", ".jsx", ".mjs", ".cjs":
		warnings = append(warnings, CheckJSBasics(content)...)
	case ".ts", ".tsx", ".mts", ".cts":
		warnings = append(warnings, CheckTSBasics(content)...)
	case ".json":
		if w := checkJSONBraces(content); w != "" {
			warnings = append(warnings, w)
		}
	}

	return strings.Join(warnings, "; ")
}

// TSLint parses a file with tree-sitter and reports syntax errors.
func TSLint(source []byte, path string) string {
	errors := TSFindErrors(source, path)
	return FormatSyntaxErrors(errors)
}

// CheckBracketBalance verifies that {}, (), and [] are balanced.
func CheckBracketBalance(content string) string {
	var stack []rune
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	lineNum := 1

	inString := false
	var stringChar rune
	escaped := false

	for _, ch := range content {
		if ch == '\n' {
			lineNum++
		}

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}

		if !inString && (ch == '"' || ch == '\'' || ch == '`') {
			inString = true
			stringChar = ch
			continue
		}
		if inString && ch == stringChar {
			inString = false
			continue
		}
		if inString {
			continue
		}

		switch ch {
		case '(', '[', '{':
			stack = append(stack, ch)
		case ')', ']', '}':
			if len(stack) == 0 {
				return fmt.Sprintf("unmatched '%c' near line %d", ch, lineNum)
			}
			if stack[len(stack)-1] != pairs[ch] {
				return fmt.Sprintf("mismatched '%c' near line %d (expected closing for '%c')", ch, lineNum, stack[len(stack)-1])
			}
			stack = stack[:len(stack)-1]
		}
	}

	if len(stack) > 0 {
		opener := stack[len(stack)-1]
		return fmt.Sprintf("unclosed '%c' — %d bracket(s) still open at end of file", opener, len(stack))
	}
	return ""
}

// checkGoBasics catches common Go syntax issues.
func checkGoBasics(filePath string) string {
	cmd := exec.Command(fmt.Sprintf("gofmt -l %s", filePath))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// JavaScript / TypeScript checks
// ---------------------------------------------------------------------------

var (
	reConsoleLog      = regexp.MustCompile(`(?m)^\s*console\.log\(`)
	reDebugger        = regexp.MustCompile(`(?m)^\s*debugger\s*;?\s*$`)
	reDuplicateImport = regexp.MustCompile(`(?m)^import\s+.*from\s+['"](.+)['"]`)
	reVarDecl         = regexp.MustCompile(`(?m)^\s*var\s+`)
	reAnyType         = regexp.MustCompile(`:\s*any\b`)
	reTSIgnore        = regexp.MustCompile(`(?m)//\s*@ts-ignore`)
)

// CheckJSBasics returns warnings for common JavaScript anti-patterns.
func CheckJSBasics(content string) []string {
	var w []string

	if locs := reDebugger.FindAllStringIndex(content, -1); len(locs) > 0 {
		w = append(w, fmt.Sprintf("'debugger' statement found (%d occurrence(s))", len(locs)))
	}

	if locs := reConsoleLog.FindAllStringIndex(content, -1); len(locs) > 5 {
		w = append(w, fmt.Sprintf("excessive console.log calls (%d found)", len(locs)))
	}

	if locs := reVarDecl.FindAllStringIndex(content, -1); len(locs) > 0 {
		w = append(w, fmt.Sprintf("'var' declaration found (%d occurrence(s)) — prefer 'const' or 'let'", len(locs)))
	}

	w = append(w, checkDuplicateImports(content)...)

	if ws := checkTemplateLiteralBalance(content); ws != "" {
		w = append(w, ws)
	}

	return w
}

// CheckTSBasics returns warnings for common TypeScript anti-patterns,
// including everything CheckJSBasics catches.
func CheckTSBasics(content string) []string {
	w := CheckJSBasics(content)

	if locs := reAnyType.FindAllStringIndex(content, -1); len(locs) > 3 {
		w = append(w, fmt.Sprintf("excessive use of 'any' type (%d occurrence(s)) — prefer specific types", len(locs)))
	}

	if locs := reTSIgnore.FindAllStringIndex(content, -1); len(locs) > 0 {
		w = append(w, fmt.Sprintf("@ts-ignore found (%d occurrence(s)) — prefer @ts-expect-error with explanation", len(locs)))
	}

	return w
}

func checkDuplicateImports(content string) []string {
	matches := reDuplicateImport.FindAllStringSubmatch(content, -1)
	if len(matches) < 2 {
		return nil
	}
	seen := map[string]int{}
	for _, m := range matches {
		seen[m[1]]++
	}
	var w []string
	for mod, count := range seen {
		if count > 1 {
			w = append(w, fmt.Sprintf("duplicate import of '%s' (%d times)", mod, count))
		}
	}
	return w
}

func checkTemplateLiteralBalance(content string) string {
	inTemplate := 0
	braceDepth := 0
	inString := false
	var strChar rune
	escaped := false

	for _, ch := range content {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}

		if inString {
			if ch == strChar {
				inString = false
			}
			continue
		}

		switch ch {
		case '"', '\'':
			inString = true
			strChar = ch
		case '`':
			if inTemplate > 0 && braceDepth == 0 {
				inTemplate--
			} else {
				inTemplate++
			}
		case '$':
			// handled below as lookahead isn't easy rune-by-rune; skip
		case '{':
			if inTemplate > 0 {
				braceDepth++
			}
		case '}':
			if inTemplate > 0 && braceDepth > 0 {
				braceDepth--
			}
		}
	}

	if inTemplate > 0 {
		return fmt.Sprintf("unclosed template literal (%d still open)", inTemplate)
	}
	return ""
}

// ---------------------------------------------------------------------------
// JSON checks
// ---------------------------------------------------------------------------

// checkJSONBraces verifies JSON files have matching top-level braces/brackets.
func checkJSONBraces(content string) string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return ""
	}
	first := trimmed[0]
	last := trimmed[len(trimmed)-1]
	if first == '{' && last != '}' {
		return "JSON object not properly closed"
	}
	if first == '[' && last != ']' {
		return "JSON array not properly closed"
	}
	return ""
}
