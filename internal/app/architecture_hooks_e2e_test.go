//go:build !windows

package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The architecture rules' hooks as claude runs them: each hook command, run
// by sh with the hook's input on stdin, against the harness (contract
// "Harness server ↔ Agent sessions — repository architecture record").

const (
	archPreMatcher  = "Edit|Write|MultiEdit|Bash"
	archPostMatcher = "Edit|Write|MultiEdit"
)

// archHooks is a shell server with a repository architecture saved over the
// API: layer domain (internal/domain), which only software-architect changes.
type archHooks struct {
	*shellE2E
	task string
	// version is the saved record's version.
	version int
}

func newArchHooks(t *testing.T, mode string) *archHooks {
	t.Helper()
	h := &archHooks{shellE2E: newShellE2E(t)}
	for _, a := range []string{"software-architect", "devops", "go-developer"} {
		if _, err := h.cat.Agents.CreateAgent(h.ctx, a, "", "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	h.task = h.shellE2E.task("Architecture hooks")
	h.save(func(r map[string]any) { r["rules"].([]any)[0].(map[string]any)["mode"] = mode })
	return h
}

// save stores the sample record, after change has adjusted it.
func (h *archHooks) save(change func(map[string]any)) {
	h.t.Helper()
	r := clone(sample())
	r["layers"] = []any{
		map[string]any{"name": "domain", "match": map[string]any{"folder": "internal/domain"},
			"access": map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change"}}},
	}
	r["rules"] = []any{map[string]any{"id": "stay-in-your-layer", "kind": "edit_outside_layer", "mode": "blocking", "message": "Ask the architect."}}
	change(r)
	var out struct {
		Version int `json:"version"`
	}
	h.post("save_repository_architecture", map[string]any{"repository_id": h.repo, "base_version": h.version, "record": r}, &out)
	h.version = out.Version
}

// as starts a session of the named agent ("" for plain).
func (h *archHooks) as(agent string) runningSession {
	h.t.Helper()
	req := ports.InteractiveRequest{ProjectID: h.project, RepositoryID: h.repo, TicketID: h.task, Mode: "implement"}
	if agent != "" {
		agents, err := h.cat.Agents.ListAgents(h.ctx)
		if err != nil {
			h.t.Fatal(err)
		}
		for _, a := range agents {
			if a.Name == agent {
				req.AgentID = a.ID
			}
		}
	}
	return h.startWith(req)
}

func additionalContext(event, text string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": text}})
	return string(b)
}

func (h *archHooks) events() []map[string]any {
	h.t.Helper()
	var out struct {
		Events []map[string]any `json:"events"`
	}
	h.post("list_architecture_rule_events", map[string]any{"repository_id": h.repo}, &out)
	return out.Events
}

func TestABlockingRuleDeniesAnEditOutsideTheAgentsLayersWithItsMessage(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")

	want := denied("Architecture rule stay-in-your-layer (blocking): Ask the architect. — internal/domain/zone.go is in layer domain, which go-developer may not change.")
	for _, tool := range []string{"Edit", "Write", "MultiEdit"} {
		if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput(tool, file)); !sameJSON(out, want) {
			t.Errorf("PreToolUse %s = %s, want %s", tool, out, want)
		}
	}
	// A relative path is read from the session's cwd.
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", "internal/domain/zone.go")); !sameJSON(out, want) {
		t.Errorf("a relative path = %s", out)
	}

	// Paths in no layer are changed by software-architect and devops only.
	unlayered := filepath.Join(a.worktree, "README.md")
	wantReadme := denied("Architecture rule stay-in-your-layer (blocking): Ask the architect. — README.md is in layer unlayered, which go-developer may not change.")
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", unlayered)); !sameJSON(out, wantReadme) {
		t.Errorf("an unlayered path = %s, want %s", out, wantReadme)
	}

	// The agent that owns the layer passes, and leaves no event.
	owner := h.as("software-architect")
	if out := runHook(t, owner, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", filepath.Join(owner.worktree, "internal", "domain", "zone.go"))); out != "" {
		t.Errorf("the layer's owner = %s, want nothing", out)
	}
	for _, ev := range h.events() {
		if ev["agent"] == "software-architect" {
			t.Errorf("a passing check left an event: %v", ev)
		}
	}

	// Outside the session's worktree nothing is checked.
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", filepath.Join(a.shell, "task", "docs", "plan.md"))); out != "" {
		t.Errorf("a path in the virtual shell = %s, want nothing", out)
	}
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", "/etc/hosts")); out != "" {
		t.Errorf("a path outside the worktree = %s, want nothing", out)
	}
}

func TestAWarningRuleLetsTheEditThroughAndSaysSoAsContext(t *testing.T) {
	h := newArchHooks(t, "warning")
	a := h.as("go-developer")
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	want := additionalContext("PreToolUse", "Architecture rule stay-in-your-layer (warning): Ask the architect. — internal/domain/zone.go is in layer domain, which go-developer may not change.")
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file)); !sameJSON(out, want) {
		t.Errorf("PreToolUse = %s, want %s", out, want)
	}
}

func TestASavedRuleProtectsRunningSessionsAtOnce(t *testing.T) {
	h := newArchHooks(t, "warning")
	a := h.as("go-developer")
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file)); out == "" {
		t.Fatal("a warning should have been given")
	}

	h.save(func(r map[string]any) { r["rules"].([]any)[0].(map[string]any)["mode"] = "blocking" })
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file)); !sameJSON(out, denied("Architecture rule stay-in-your-layer (blocking): Ask the architect. — internal/domain/zone.go is in layer domain, which go-developer may not change.")) {
		t.Errorf("after the save to blocking: %s", out)
	}

	h.save(func(r map[string]any) {
		r["layers"].([]any)[0].(map[string]any)["access"] = map[string]any{"default": "read", "agents": map[string]any{"go-developer": "change"}}
	})
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file)); out != "" {
		t.Errorf("after the access was granted: %s", out)
	}

	// Deleting the record stops the rules.
	h.post("delete_repository_architecture", map[string]any{"repository_id": h.repo}, nil)
	h.version = 0
	if out := runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", filepath.Join(a.worktree, "README.md"))); out != "" {
		t.Errorf("after the delete: %s", out)
	}
}

func TestAnUnreachableHarnessLetsTheEditThroughAndTellsTheAgent(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	const down = "http://127.0.0.1:1"
	want := additionalContext("PreToolUse", "Architecture rules could not be checked: the harness could not be reached.")
	if out := runHook(t, a, down, "PreToolUse", archPreMatcher, editInput("Edit", file)); !sameJSON(out, want) {
		t.Errorf("PreToolUse with the harness down = %s, want %s", out, want)
	}
}

func TestARuleAppliesToTheAgentsItNamesAndPlainSessionsAreAnAgentToo(t *testing.T) {
	h := newArchHooks(t, "blocking")
	h.save(func(r map[string]any) {
		r["rules"].([]any)[0].(map[string]any)["agents"] = []any{"go-developer"}
		r["layers"].([]any)[0].(map[string]any)["access"] = map[string]any{"default": "none", "agents": map[string]any{"plain": "read"}}
	})
	devops, plain := h.as("devops"), h.as("")
	file := func(s runningSession) string { return filepath.Join(s.worktree, "internal", "domain", "zone.go") }
	if out := runHook(t, devops, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file(devops))); out != "" {
		t.Errorf("an agent the rule does not name = %s, want nothing", out)
	}
	if out := runHook(t, plain, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file(plain))); out != "" {
		t.Errorf("a plain session under a rule for go-developer = %s, want nothing", out)
	}

	h.save(func(r map[string]any) { delete(r["rules"].([]any)[0].(map[string]any), "agents") })
	want := denied("Architecture rule stay-in-your-layer (blocking): Ask the architect. — internal/domain/zone.go is in layer domain, which plain may not change.")
	if out := runHook(t, plain, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file(plain))); !sameJSON(out, want) {
		t.Errorf("a plain session = %s, want %s", out, want)
	}

	// A record with no edit_outside_layer rule behaves as a warning rule with
	// the default message.
	h.save(func(r map[string]any) { r["rules"] = []any{} })
	wantDefault := additionalContext("PreToolUse", "Architecture rule stay-in-your-layer (warning): "+domain.DefaultEditOutsideLayerMessage+" — internal/domain/zone.go is in layer domain, which plain may not change.")
	if out := runHook(t, plain, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file(plain))); !sameJSON(out, wantDefault) {
		t.Errorf("no rule saved = %s, want %s", out, wantDefault)
	}
}

func TestEveryFailedRuleIsRecordedAndAnnounced(t *testing.T) {
	h := newArchHooks(t, "blocking")
	feed := &archE2E{t: t, srv: h.srv, project: h.project}
	feed.follow()
	a := h.as("go-developer")
	file := filepath.Join(a.worktree, "internal", "domain", "zone.go")
	runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Edit", file))
	h.save(func(r map[string]any) { r["rules"].([]any)[0].(map[string]any)["mode"] = "warning" })
	runHook(t, a, h.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", filepath.Join(a.worktree, "README.md")))

	events := h.events()
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	newest, oldest := events[0], events[1]
	if newest["path"] != "README.md" || newest["layer"] != "unlayered" || newest["tool"] != "Write" || newest["mode"] != "warning" || newest["outcome"] != "warned" {
		t.Errorf("newest = %v", newest)
	}
	if oldest["path"] != "internal/domain/zone.go" || oldest["layer"] != "domain" || oldest["tool"] != "Edit" || oldest["mode"] != "blocking" || oldest["outcome"] != "blocked" {
		t.Errorf("oldest = %v", oldest)
	}
	for _, ev := range events {
		if ev["agent"] != "go-developer" || ev["session_id"] != a.id || ev["task_id"] != h.task || ev["rule_id"] != "stay-in-your-layer" ||
			ev["kind"] != "edit_outside_layer" || ev["at_point"] != "before_tool" || ev["id"] == "" || ev["at"] == "" || ev["detail"] != "" {
			t.Errorf("event = %v", ev)
		}
	}
	feed.sawOnFeed("architecture_rule_event", `"repository_id":"`+h.repo+`"`, `"path":"README.md"`, `"outcome":"warned"`)

	// since keeps only what came after it; limit caps the newest.
	var out struct {
		Events []map[string]any `json:"events"`
	}
	h.post("list_architecture_rule_events", map[string]any{"repository_id": h.repo, "since": oldest["at"]}, &out)
	if len(out.Events) != 1 || out.Events[0]["path"] != "README.md" {
		t.Errorf("since the oldest = %v", out.Events)
	}
	h.post("list_architecture_rule_events", map[string]any{"repository_id": h.repo, "limit": 1}, &out)
	if len(out.Events) != 1 || out.Events[0]["path"] != "README.md" {
		t.Errorf("limit 1 = %v", out.Events)
	}
}

// gitIn runs git in dir and answers its combined output and error.
func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "user.email=agent@test", "-c", "user.name=agent", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// pointCommitCheckAt aims the session worktree's pre-commit hook at base
// instead of the configured harness address, the way runHook does for the
// claude hooks.
func pointCommitCheckAt(t *testing.T, worktree, base string) {
	t.Helper()
	dir, err := gitIn(worktree, "config", "--get", "core.hooksPath")
	if err != nil {
		t.Fatalf("the worktree has no core.hooksPath: %v %s", err, dir)
	}
	hook := filepath.Join(dir, "pre-commit")
	script, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), configuredBase) {
		t.Fatalf("the pre-commit hook does not call the harness at %s:\n%s", configuredBase, script)
	}
	if err := os.WriteFile(hook, []byte(strings.ReplaceAll(string(script), configuredBase, base)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stageAndCommit stages files (path → content) in dir and commits them.
func stageAndCommit(t *testing.T, dir string, files map[string]string) (string, error) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := gitIn(dir, "add", "--", p); err != nil {
			t.Fatalf("git add: %v %s", err, out)
		}
	}
	return gitIn(dir, "commit", "-q", "-m", "agent change")
}

func TestABlockingRuleFailsACommitThatStagesAFileOutsideTheAgentsLayers(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	head, _ := gitIn(a.worktree, "rev-parse", "HEAD")

	out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n", "docs/notes.md": "hi\n"})
	if err == nil {
		t.Fatalf("the commit went through: %s", out)
	}
	for _, want := range []string{
		"Architecture rule stay-in-your-layer (blocking): Ask the architect. — internal/domain/zone.go is in layer domain, which go-developer may not change.",
		"Architecture rule stay-in-your-layer (blocking): Ask the architect. — docs/notes.md is in layer unlayered, which go-developer may not change.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("git printed %q, want %q", out, want)
		}
	}
	if now, _ := gitIn(a.worktree, "rev-parse", "HEAD"); now != head {
		t.Error("HEAD moved")
	}
	events := h.events()
	if len(events) != 2 || events[0]["at_point"] != "before_commit" || events[0]["tool"] != "git commit" || events[0]["outcome"] != "blocked" || events[0]["agent"] != "go-developer" {
		t.Errorf("events = %v", events)
	}
}

func TestAWarningRulePrintsItAndLetsTheCommitGo(t *testing.T) {
	h := newArchHooks(t, "warning")
	a := h.as("go-developer")
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n"})
	if err != nil {
		t.Fatalf("the commit failed: %v %s", err, out)
	}
	if want := "Architecture rule stay-in-your-layer (warning): Ask the architect. — internal/domain/zone.go is in layer domain, which go-developer may not change."; !strings.Contains(out, want) {
		t.Errorf("git printed %q, want %q", out, want)
	}
	if events := h.events(); len(events) != 1 || events[0]["outcome"] != "warned" || events[0]["at_point"] != "before_commit" {
		t.Errorf("events = %v", events)
	}
}

func TestACommitThatStagesOnlyAllowedPathsPassesAndLeavesNoEvent(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("software-architect")
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	if out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n", "docs/notes.md": "hi\n"}); err != nil {
		t.Fatalf("the commit failed: %v %s", err, out)
	}
	if events := h.events(); len(events) != 0 {
		t.Errorf("a passing commit left events: %v", events)
	}
	// A commit that names its paths (a temporary index) is checked too.
	b := h.as("go-developer")
	pointCommitCheckAt(t, b.worktree, h.srv.URL)
	if err := os.MkdirAll(filepath.Join(b.worktree, "internal", "domain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.worktree, "internal", "domain", "x.go"), []byte("package domain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := gitIn(b.worktree, "add", "-N", "internal/domain/x.go"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := gitIn(b.worktree, "commit", "-q", "-m", "x", "internal/domain/x.go"); err == nil || !strings.Contains(out, "internal/domain/x.go is in layer domain") {
		t.Errorf("commit with paths: %v %s", err, out)
	}
}

func TestTheCommitCheckLeavesThePersonsAndTheServersCommitsAlone(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	repo, err := h.cat.Projects.GetRepository(h.ctx, h.repo)
	if err != nil {
		t.Fatal(err)
	}
	// The repository's own pre-commit hook still runs in the session worktree.
	hooks, _ := gitIn(repo.RootDir, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(repo.RootDir, hooks)
	}
	marker := filepath.Join(t.TempDir(), "own-hook-ran")
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pointCommitCheckAt(t, a.worktree, h.srv.URL)
	head, _ := gitIn(repo.RootDir, "rev-parse", "HEAD")
	status0, _ := gitIn(repo.RootDir, "status", "--porcelain")

	// The person commits in the main checkout: not checked.
	if out, err := stageAndCommit(t, repo.RootDir, map[string]string{"internal/domain/mine.go": "package domain\n"}); err != nil {
		t.Fatalf("the person's commit failed: %v %s", err, out)
	}
	// The server's own export commit is not checked either.
	h.post("export_repository_architecture", map[string]any{"repository_id": h.repo, "version": h.version}, nil)
	if now, _ := gitIn(repo.RootDir, "rev-parse", "HEAD"); now == head {
		t.Error("nothing was committed in the main checkout")
	}
	if events := h.events(); len(events) != 0 {
		t.Errorf("commits outside the session worktrees left events: %v", events)
	}
	if status, _ := gitIn(repo.RootDir, "status", "--porcelain"); status != status0 {
		t.Errorf("the checkout's tracked files or status changed: %q", status)
	}
	// An allowed-looking commit in the session worktree runs the repository's hook after the check.
	h.save(func(r map[string]any) { r["rules"] = []any{} })
	if out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n"}); err != nil {
		t.Fatalf("a warning-mode commit failed: %v %s", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the repository's own pre-commit hook did not run: %v", err)
	}
}

func TestAnUnreachableHarnessLetsTheCommitGoAndSaysSo(t *testing.T) {
	h := newArchHooks(t, "blocking")
	a := h.as("go-developer")
	pointCommitCheckAt(t, a.worktree, "http://127.0.0.1:1")
	out, err := stageAndCommit(t, a.worktree, map[string]string{"internal/domain/zone.go": "package domain\n"})
	if err != nil {
		t.Fatalf("the commit failed: %v %s", err, out)
	}
	if !strings.Contains(out, "Architecture rules could not be checked: the harness could not be reached.") {
		t.Errorf("git printed %q", out)
	}
}
