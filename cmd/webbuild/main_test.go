package main

import (
	"os"
	"strings"
	"testing"
)

// web/test/all.js must list every *.test.js, or the browser suite silently
// skips a file. Regenerate with `go run ./cmd/webbuild`.
func TestTestListIsCurrent(t *testing.T) {
	want, err := TestList("../..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../" + testsList)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; run `go run ./cmd/webbuild`.\nwant:\n%s", testsList, want)
	}
	if !strings.Contains(want, `import "../src/modules/sessions/domain/session.test.js";`) {
		t.Fatalf("expected the sessions domain tests in the list:\n%s", want)
	}
}

func TestOptionsAliasAlpineToTheVendoredBuild(t *testing.T) {
	opts := options("/repo", false)
	if opts.Alias["alpinejs"] != alpineAlias {
		t.Fatalf("alias = %q", opts.Alias["alpinejs"])
	}
	if !opts.MinifyWhitespace || options("/repo", true).MinifyWhitespace {
		t.Fatal("production minifies; dev does not")
	}
	for name, path := range vendors {
		if _, err := os.Stat("../../" + strings.TrimPrefix(path, "./")); err != nil {
			t.Fatalf("vendored %s missing: %v", name, err)
		}
	}
}
