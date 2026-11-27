//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The context map of a repository, read through the API the instruction-files
// UI uses: generated from the default branch, stamped with its commit,
// regenerated when the branch moves, off when the configuration says so.

type contextMapBody struct {
	RepositoryID string `json:"repository_id"`
	Enabled      bool   `json:"enabled"`
	Available    bool   `json:"available"`
	Commit       string `json:"commit"`
	Content      string `json:"content"`
}

func (e *shellE2E) contextMapOf(repo string) (int, contextMapBody, string) {
	e.t.Helper()
	b, _ := json.Marshal(map[string]string{"repository_id": repo})
	status, text := postJSON(e.t, e.srv.URL+"/api/get_context_map", string(b))
	var out contextMapBody
	_ = json.Unmarshal([]byte(text), &out)
	return status, out, text
}

// commitFile commits a file to the main checkout's default branch and answers
// the new tip.
func (e *shellE2E) commitFile(rel, content string) string {
	e.t.Helper()
	p := filepath.Join(e.repoDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "-m", "add " + rel}, {"rev-parse", "HEAD"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = e.repoDir
		out, err := cmd.Output() // stdout only: git may complain on stderr in a sandbox
		if err != nil {
			e.t.Fatalf("git %v: %v", args, err)
		}
		if args[0] == "rev-parse" {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func TestContextMap_IsGeneratedFromTheDefaultBranchAndStampedWithItsCommit(t *testing.T) {
	e := newShellE2E(t)
	e.commitFile("go.mod", "module widgets\n\ngo 1.25\n")
	e.commitFile("cmd/widgets/main.go", "package main\n\nfunc main() {}\n")
	tip := e.commitFile("AGENTS.md", "HAND-WRITTEN-RULE: always run the tests\n")

	status, got, text := e.contextMapOf(e.repo)
	if status != 200 || !got.Enabled || !got.Available || got.Commit != tip {
		t.Fatalf("get = %d %s, want commit %s", status, text, tip)
	}
	for _, want := range []string{"# Context map: harness", "@ " + tip, "- `cmd/widgets/main.go`", "## Commands", "go test"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the map lacks %q:\n%s", want, got.Content)
		}
	}
	if strings.Contains(got.Content, "HAND-WRITTEN-RULE") {
		t.Errorf("the map repeats AGENTS.md")
	}
	if lines := strings.Count(got.Content, "\n"); lines > 300 {
		t.Errorf("the map has %d lines", lines)
	}
	// Never committed, never in the checkout: reading the map leaves it clean.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = e.repoDir
	if out, err := cmd.Output(); err != nil || len(out) != 0 {
		t.Errorf("the checkout is not clean after a read: %q, %v", out, err)
	}
}

func TestContextMap_IsRegeneratedWhenTheDefaultBranchMovesAndAnnounced(t *testing.T) {
	e := newShellE2E(t)
	feed := &archE2E{t: t, ctx: e.ctx, srv: e.srv, project: e.project}
	feed.follow()
	first := e.commitFile("main.go", "package main\n\nfunc main() {}\n")
	_, a, _ := e.contextMapOf(e.repo)
	if a.Commit != first {
		t.Fatalf("first commit = %q, want %s", a.Commit, first)
	}
	feed.sawOnFeed("context_map_regenerated", `"repository_id":"`+e.repo+`"`, `"commit":"`+first+`"`)

	// The branch has not moved: the same map, no second announcement.
	if _, again, _ := e.contextMapOf(e.repo); again.Commit != first || again.Content != a.Content {
		t.Errorf("a read with the branch where it was changed the map")
	}
	second := e.commitFile("internal/newpkg/x.go", "package newpkg\n")
	_, b, _ := e.contextMapOf(e.repo)
	if b.Commit != second || !strings.Contains(b.Content, "internal/newpkg/") {
		t.Fatalf("after the branch moved: commit %q, content:\n%s", b.Commit, b.Content)
	}
	feed.sawOnFeed("context_map_regenerated", `"commit":"`+second+`"`)
}

func TestContextMap_IsOffWhenTheEffectiveConfigurationSaysSo(t *testing.T) {
	e := newShellE2E(t)
	e.commitFile("main.go", "package main\n")
	if _, err := e.agentSystem.SaveRepository(e.ctx, e.project, e.repo, map[string]json.RawMessage{"learn.context_map.enabled": json.RawMessage("false")}); err != nil {
		t.Fatal(err)
	}
	status, got, text := e.contextMapOf(e.repo)
	if status != 200 || got.Enabled || got.Available || got.Content != "" {
		t.Fatalf("a disabled map = %d %s", status, text)
	}
	if status, _, text := e.contextMapOf("nope"); status != 404 || !strings.Contains(text, "REPOSITORY_NOT_FOUND") {
		t.Errorf("an unknown repository = %d %s", status, text)
	}
}

// Delivered beside the instruction files at the start of a session on the
// repository, resolved once and recorded in the context trail.

func deliveredMap(t *testing.T, e *shellE2E) (content string, found bool) {
	t.Helper()
	id, spec := e.startRaw(e.task("Map delivery " + t.Name()))
	for _, f := range spec.InstructionFiles {
		if f.Path == "CONTEXT-MAP.md" {
			content, found = string(f.Content), true
		}
	}
	view, err := e.orch.SessionContext(e.ctx, id)
	if err != nil || len(view.Entries) == 0 {
		t.Fatalf("context = %+v, %v", view, err)
	}
	var inTrail bool
	for _, p := range view.Entries[0].Instructions {
		if p.Path == "CONTEXT-MAP.md" {
			inTrail = true
		}
	}
	if inTrail != found {
		t.Errorf("the map is delivered = %v but in the context trail = %v", found, inTrail)
	}
	return content, found
}

func TestContextMap_IsInTheStartingContextOfASessionOnTheRepository(t *testing.T) {
	e := newShellE2E(t)
	tip := e.commitFile("main.go", "package main\n")
	got, found := deliveredMap(t, e)
	if !found || !strings.Contains(got, "# Context map: harness") || !strings.Contains(got, tip) {
		t.Fatalf("the starting context lacks the map: found=%v\n%s", found, got)
	}
}

func TestContextMap_IsNotInTheStartingContextWhenTheConfigurationTurnsItOff(t *testing.T) {
	e := newShellE2E(t)
	e.commitFile("main.go", "package main\n")
	if _, err := e.agentSystem.SaveRepository(e.ctx, e.project, e.repo, map[string]json.RawMessage{"learn.context_map.enabled": json.RawMessage("false")}); err != nil {
		t.Fatal(err)
	}
	if got, found := deliveredMap(t, e); found {
		t.Fatalf("a disabled map was delivered:\n%s", got)
	}
}

// A change to a gate command moves what the map says, so it regenerates even
// though the default branch did not move.
func TestContextMap_IsRegeneratedWhenAGateCommandChanges(t *testing.T) {
	e := newShellE2E(t)
	tip := e.commitFile("go.mod", "module widgets\n\ngo 1.25\n")
	_, before, _ := e.contextMapOf(e.repo)
	if strings.Contains(before.Content, "make verify-everything") {
		t.Fatal("the gate is there before it was set")
	}
	if _, err := e.agentSystem.SaveRepository(e.ctx, e.project, e.repo, map[string]json.RawMessage{"guard.gates.test.command": json.RawMessage(`"make verify-everything"`)}); err != nil {
		t.Fatal(err)
	}
	_, after, _ := e.contextMapOf(e.repo)
	if after.Commit != tip || !strings.Contains(after.Content, "make verify-everything") {
		t.Errorf("after the gate change (commit %s): %s", after.Commit, after.Content)
	}
}
