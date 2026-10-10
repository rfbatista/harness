package gitcli

import (
	"bufio"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

const samplePatch = `diff --git a/app.go b/app.go
index 1..2 100644
--- a/app.go
+++ b/app.go
@@ -1,3 +1,4 @@ func Run() {
 package app
 
+// Run runs.
 func Run() {}
diff --git a/docs/old.md b/docs/new.md
similarity index 88%
rename from docs/old.md
rename to docs/new.md
index 3..4 100644
--- a/docs/old.md
+++ b/docs/new.md
@@ -7,2 +7,3 @@
 five
 six
+seven
diff --git a/moved b/moved2
similarity index 100%
rename from moved
rename to moved2
diff --git a/nonl.txt b/nonl.txt
deleted file mode 100644
index 5..0
--- a/nonl.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-first
-last
\ No newline at end of file
diff --git "a/tab\there" "b/tab\there"
new file mode 100644
--- /dev/null
+++ "b/tab\there"
@@ -0,0 +1 @@
+x
diff --git a/with space.txt b/with space.txt
--- a/with space.txt	
+++ b/with space.txt	
@@ -1 +1 @@
-a
+b
diff --git a/logo.png b/logo.png
new file mode 100644
index 0..6
Binary files /dev/null and b/logo.png differ
`

func parse(t *testing.T, patch string, maxLines int, maxBytes int64, single bool) map[string]*domain.FileDiff {
	t.Helper()
	out := map[string]*domain.FileDiff{}
	err := parsePatch(bufio.NewReader(strings.NewReader(patch)), maxLines, maxBytes, single, out)
	if err != nil && err != errPatchDone {
		t.Fatal(err)
	}
	return out
}

func TestParsePatch(t *testing.T) {
	out := parse(t, samplePatch, 100, 1<<20, false)
	var keys []string
	for k := range out {
		keys = append(keys, k)
	}
	if len(out) != 7 {
		t.Fatalf("files = %v", keys)
	}
	app := out["app.go"]
	if len(app.Hunks) != 1 || app.Hunks[0].Header != "func Run() {" || len(app.Hunks[0].Lines) != 4 {
		t.Fatalf("app.go = %+v", app)
	}
	l := app.Hunks[0].Lines
	if l[1].Kind != domain.LineContext || l[1].Text != "" || *l[1].Old != 2 || *l[1].New != 2 {
		t.Errorf("empty context line = %+v", l[1])
	}
	if l[2].Kind != domain.LineAdd || *l[2].New != 3 || l[2].Old != nil || l[3].Kind != domain.LineContext || *l[3].Old != 3 || *l[3].New != 4 {
		t.Errorf("lines = %+v", l)
	}
	if g := out["docs/new.md"]; g == nil || g.Hunks[0].OldStart != 7 || g.Hunks[0].OldLines != 2 || g.Hunks[0].NewLines != 3 {
		t.Errorf("rename = %+v", g)
	}
	if m := out["moved2"]; m == nil || m.Hunks != nil {
		t.Errorf("pure rename = %+v", m)
	}
	d := out["nonl.txt"]
	if d == nil || d.Hunks[0].NewStart != 0 || len(d.Hunks[0].Lines) != 2 || !d.Hunks[0].Lines[1].NoNewline || d.Hunks[0].Lines[0].NoNewline {
		t.Errorf("deletion = %+v", d)
	}
	if q := out["tab\there"]; q == nil || q.Hunks[0].NewLines != 1 || q.Hunks[0].OldLines != 0 {
		t.Errorf("quoted name = %+v", q)
	}
	if s := out["with space.txt"]; s == nil || len(s.Hunks) != 1 {
		t.Errorf("name with a space = %+v", s)
	}
	if b := out["logo.png"]; b == nil || !b.Binary || b.Withheld == nil || *b.Withheld != domain.WithheldBinary {
		t.Errorf("binary = %+v", b)
	}
}

func TestParsePatchWithholdsAFilePastItsCaps(t *testing.T) {
	out := parse(t, samplePatch, 1, 1<<20, false)
	if a := out["app.go"]; a.Withheld != nil || len(a.Hunks) != 1 {
		t.Errorf("app.go at 1 changed line = %+v", a)
	}
	if d := out["nonl.txt"]; d.Withheld == nil || *d.Withheld != domain.WithheldTooLarge || d.Hunks != nil {
		t.Errorf("nonl.txt past 1 line = %+v", d)
	}
	bytesOut := parse(t, samplePatch, 100, 10, false)
	if a := bytesOut["app.go"]; a.Withheld == nil {
		t.Errorf("app.go past 10 bytes = %+v", a)
	}
	single := parse(t, samplePatch, 1, 1<<20, true)
	if single["nonl.txt"].Withheld == nil || single["with space.txt"] != nil {
		t.Errorf("reading goes on past the withheld file: %d files", len(single))
	}
	long := "diff --git a/min.js b/min.js\n--- a/min.js\n+++ b/min.js\n@@ -1 +1 @@\n+" + strings.Repeat("x", 200_000) + "\n"
	if m := parse(t, long, 100, 1000, false)["min.js"]; m.Withheld == nil || m.Hunks != nil {
		t.Errorf("a long line = %+v", m)
	}
}

func TestUnquote(t *testing.T) {
	for in, want := range map[string]string{`"a\tb"`: "a\tb", `"caf\303\251"`: "café", `"q\"x"`: `q"x`, `plain`: "plain", `"b\\s"`: `b\s`} {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%s) = %q, want %q", in, got, want)
		}
	}
}
