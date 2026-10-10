package domain

import (
	"reflect"
	"testing"
)

func TestCleanCodePath(t *testing.T) {
	ok := map[string]string{"": "", "a": "a", "a/b.go": "a/b.go", "dir/": "dir", ".github/x": ".github/x", "a b/c": "a b/c"}
	for in, want := range ok {
		if got, valid := CleanCodePath(in); !valid || got != want {
			t.Errorf("CleanCodePath(%q) = %q, %v; want %q", in, got, valid, want)
		}
	}
	for _, in := range []string{"/etc/passwd", "../x", "a/../b", "a/./b", "a//b", ".git", ".git/config", "a/.GIT/x", "a\x00b", `a\b`, "./a", "."} {
		if _, valid := CleanCodePath(in); valid {
			t.Errorf("CleanCodePath(%q) accepted", in)
		}
	}
}

func TestValidRefName(t *testing.T) {
	for _, r := range []string{"main", "HEAD", "feature/x", "origin/main", "refs/heads/main", "v1.0.0", "deadbeef", "agent/visualize-code-9424f6e4"} {
		if !ValidRefName(r) {
			t.Errorf("ValidRefName(%q) = false", r)
		}
	}
	for _, r := range []string{"", "-n", "--output=x", "main~1", "HEAD^", "@{u}", "main@{1}", "a..b", "a:b", "a b", "a*", "a?", "a[", `a\b`,
		"x.lock", "x/", "/x", ".hidden", "a/.b", "a//b", "@", "a.", "a\x01"} {
		if ValidRefName(r) {
			t.Errorf("ValidRefName(%q) = true", r)
		}
	}
}

func TestLanguageOf(t *testing.T) {
	cases := map[string]string{
		"main.go": "go", "web/x.JS": "javascript", "page.templ": "templ", "README.md": "markdown",
		"Makefile": "makefile", "go.mod": "gomod", "Dockerfile.dev": "dockerfile", "LICENSE": "plaintext", "x.unknown": "plaintext",
	}
	for in, want := range cases {
		if got := LanguageOf(in); got != want {
			t.Errorf("LanguageOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestZoneMatcher(t *testing.T) {
	zones := []*Zone{
		{ID: "api", Pattern: "^internal/adapter/in/"},
		{ID: "web", ExplicitPaths: []string{"web"}},
		{ID: "empty"},
		{ID: "bad", Pattern: "("},
	}
	m := NewZoneMatcher(zones)
	cases := map[string][]string{
		"internal/adapter/in/httpapi/code.go": {"api"},
		"web":                                 {"web"},
		"web/src/main.js":                     {"web"},
		"webby/x":                             {},
		"README.md":                           {},
	}
	for p, want := range cases {
		if got := m.Match(p); !reflect.DeepEqual(got, want) {
			t.Errorf("Match(%q) = %v, want %v", p, got, want)
		}
	}
	if (&Zone{}).Contains("anything") {
		t.Error("a zone with no pattern and no paths holds a path")
	}
}

func TestSymlinkEscapes(t *testing.T) {
	cases := map[[2]string]bool{
		{"link", "README.md"}: false, {"a/link", "../b"}: false, {"a/link", "../../x"}: true,
		{"link", "../x"}: true, {"link", "/etc"}: true, {"link", ".."}: true, {"a/b/l", "../../c"}: false, {"link", ""}: true,
	}
	for c, want := range cases {
		if got := SymlinkEscapes(c[0], c[1]); got != want {
			t.Errorf("SymlinkEscapes(%q, %q) = %v, want %v", c[0], c[1], got, want)
		}
	}
}

func TestIsBinaryAndCountLines(t *testing.T) {
	if IsBinary([]byte("plain\ntext")) || !IsBinary([]byte("a\x00b")) || !IsBinary([]byte{0xff, 0xfe}) || IsBinary(nil) {
		t.Error("IsBinary misjudged")
	}
	for in, want := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "\n\n": 2} {
		if got := CountLines(in); got != want {
			t.Errorf("CountLines(%q) = %d, want %d", in, got, want)
		}
	}
}
