package contextmap

import (
	"fmt"
	"strings"
	"testing"
)

func widgets() Input {
	files := []string{
		"AGENTS.md", "README.md", "Makefile", "go.mod",
		"cmd/server/main.go", "cmd/server/flags.go", "cmd/tool/main.go",
		"internal/domain/task.go", "internal/domain/session.go", "internal/app/app.go",
		"web/src/app.ts", "web/package.json",
	}
	read := map[string]string{
		"AGENTS.md":        "SECRET-HAND-WRITTEN-RULE: never touch prod\n",
		"web/package.json": `{"name":"web","main":"src/app.ts"}`,
	}
	return Input{
		Repository: "widgets", Branch: "main", Commit: "abc1234def",
		Files: files,
		Read: func(p string) ([]byte, bool) {
			s, ok := read[p]
			return []byte(s), ok
		},
		Gates:        []Gate{{Kind: "build", Command: "go build ./..."}, {Kind: "test", Command: "go test ./..."}},
		Instructions: []string{"AGENTS.md", "web/AGENTS.md"},
		Layers:       []Layer{{Name: "domain", Folder: "internal/domain"}},
	}
}

func TestGenerateHoldsLayoutEntryPointsCommandsAndWhereToLook(t *testing.T) {
	got := Generate(widgets())
	for _, want := range []string{
		"# Context map: widgets",
		"main @ abc1234def",
		"## Commands",
		"- build: `go build ./...`",
		"- test: `go test ./...`",
		"## Layout",
		"- `cmd/server/` — 2 files",
		"- `internal/domain/` — 2 files",
		"## Entry points",
		"- `cmd/server/main.go`",
		"- `cmd/tool/main.go`",
		"- `web/src/app.ts` (web/package.json main)",
		"- `Makefile`",
		"## Where to look",
		"- `AGENTS.md` — hand-written guidance",
		"- `internal/domain/` — layer domain",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the map lacks %q:\n%s", want, got)
		}
	}
}

func TestGeneratePointsToAGENTSAndNeverRepeatsIt(t *testing.T) {
	got := Generate(widgets())
	if strings.Contains(got, "SECRET-HAND-WRITTEN-RULE") {
		t.Errorf("the map repeats AGENTS.md:\n%s", got)
	}
	in := widgets()
	in.Instructions = nil
	if got := Generate(in); strings.Contains(got, "hand-written guidance") {
		t.Errorf("a repository with no instruction files is pointed to one:\n%s", got)
	}
}

func TestGenerateStaysWithinTheLineCapOnALargeTree(t *testing.T) {
	in := widgets()
	for i := 0; i < 600; i++ {
		in.Files = append(in.Files, fmt.Sprintf("pkg/p%03d/sub/file.go", i), fmt.Sprintf("pkg/p%03d/doc.go", i))
	}
	got := Generate(in)
	if n := len(strings.Split(strings.TrimRight(got, "\n"), "\n")); n > MaxLines {
		t.Errorf("the map has %d lines, cap %d", n, MaxLines)
	}
	if !strings.Contains(got, "more directories") {
		t.Errorf("a cut layout does not say so")
	}
	// What the cap cuts is the layout, never the commands or the pointers.
	for _, want := range []string{"- test: `go test ./...`", "- `AGENTS.md` — hand-written guidance"} {
		if !strings.Contains(got, want) {
			t.Errorf("the cap cut %q", want)
		}
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	if a, b := Generate(widgets()), Generate(widgets()); a != b {
		t.Errorf("two runs differ")
	}
}
