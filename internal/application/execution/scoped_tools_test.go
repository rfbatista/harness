package execution

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// legacyIsPathAllowedByZone is the rule as execution held it before the
// domain owned zone membership; isPathAllowedByZone must agree with it.
func legacyIsPathAllowedByZone(relPath, pattern string, explicitPaths []string) bool {
	if pattern == "" && len(explicitPaths) == 0 {
		return true
	}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err == nil && re.MatchString(relPath) {
			return true
		}
	}
	for _, ep := range explicitPaths {
		if relPath == ep || strings.HasPrefix(relPath, ep+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func TestZoneBoundaryIsUnchangedByTheDomainRule(t *testing.T) {
	zones := []struct {
		pattern  string
		explicit []string
	}{
		{"", nil}, // an empty zone allows everything
		{"", []string{}},
		{"^internal/", nil},
		{"handlers", nil},      // unanchored
		{`\.go$`, nil},         // suffix
		{"(", nil},             // invalid pattern: matches nothing
		{"(", []string{"web"}}, // invalid pattern, explicit path still counts
		{"", []string{"web", "cmd/server"}},
		{"^docs/", []string{"web/src"}},
		{".*", nil},
	}
	paths := []string{
		"", ".", "internal", "internal/app/server.go", "internal2/x.go", "cmd/server",
		"cmd/server/main.go", "cmd/serverless/main.go", "web", "web/src/main.js", "webby/x",
		"docs/a.md", "handlers.go", "pkg/handlers/x.go", "README", "a/b/c.go",
	}
	for _, z := range zones {
		for _, p := range paths {
			want := legacyIsPathAllowedByZone(p, z.pattern, z.explicit)
			if got := isPathAllowedByZone(p, z.pattern, z.explicit); got != want {
				t.Errorf("zone{pattern %q, explicit %q} path %q: allowed = %v, was %v", z.pattern, z.explicit, p, got, want)
			}
		}
	}
}
