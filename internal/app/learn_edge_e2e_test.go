//go:build !windows

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Learn edge cases at the server boundary: the HTTP API (seam C) and a fake
// agent host driving sessions (seam B).

func writeMemoryOnDisk(t *testing.T, spec ports.AgentSpec, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(memoryDirOf(t, spec), file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func progressWith(done string) string {
	return "# Progress\n\n## Done\n\n" + done + "\n\n## Failed and why\n\n## Next step\n\n## Open questions\n"
}

// ---- C: the HTTP API ----

func TestLearnAPI_ContextMapRefusals(t *testing.T) {
	e := newShellE2E(t)
	for name, body := range map[string]string{
		"no repository id": `{}`,
		"empty id":         `{"repository_id":""}`,
		"unknown id":       `{"repository_id":"nope"}`,
	} {
		status, text := postJSON(t, e.srv.URL+"/api/get_context_map", body)
		if status != 404 && status != 400 {
			t.Errorf("%s = %d %s, want 400 or 404", name, status, text)
		}
		if status >= 500 {
			t.Errorf("%s answered a server error: %s", name, text)
		}
	}
	for name, body := range map[string]string{"not json": `{`, "wrong type": `{"repository_id": 7}`, "empty body": ``} {
		if status, text := postJSON(t, e.srv.URL+"/api/get_context_map", body); status != 400 {
			t.Errorf("%s = %d %s, want 400", name, status, text)
		}
	}
}

func TestLearnAPI_ContextMapOfARepositoryWithNoDefaultBranchIsEnabledButNotAvailable(t *testing.T) {
	e := newShellE2E(t)
	unborn := filepath.Join(t.TempDir(), "unborn")
	if err := os.MkdirAll(unborn, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = unborn
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	notGit := t.TempDir()
	for name, dir := range map[string]string{"unborn branch": unborn, "not a git directory": notGit, "missing directory": filepath.Join(notGit, "gone")} {
		r, err := e.cat.Projects.CreateRepository(e.ctx, e.project, "r-"+strings.ReplaceAll(name, " ", "-"), "", "https://example.com/x.git", dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		status, got, text := e.contextMapOf(r.ID)
		if status != 200 || !got.Enabled || got.Available || got.Content != "" {
			t.Errorf("%s = %d %s, want enabled, not available, no content", name, status, text)
		}
	}
}

func TestLearnAPI_ContextMapOfARepositoryWithoutAGENTSAndVendoredDirectoriesIsWithinTheCap(t *testing.T) {
	e := newShellE2E(t)
	for i := 0; i < 400; i++ {
		p := filepath.Join(e.repoDir, "node_modules", "pkg"+strings.Repeat("x", i%5), "d"+string(rune('a'+i%26)), "f"+strings.Repeat("y", i%7)+".js")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("module.exports=1\n"), 0o644)
	}
	if err := os.WriteFile(filepath.Join(e.repoDir, "blob.bin"), []byte{0, 1, 2, 0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	e.commitFile("main.go", "package main\n")
	status, got, text := e.contextMapOf(e.repo)
	if status != 200 || !got.Available {
		t.Fatalf("get = %d %s", status, text)
	}
	if n := strings.Count(got.Content, "\n"); n > 300 {
		t.Errorf("map has %d lines", n)
	}
	if strings.Contains(got.Content, "hand-written guidance") {
		t.Errorf("a repository with no AGENTS.md is pointed to one:\n%s", got.Content)
	}
}

func TestLearnAPI_TaskMemoryRefusals(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Refusals")
	for name, tc := range map[string]struct{ task, repo string }{
		"no ids": {"", ""}, "no task": {"", e.repo}, "no repository": {task, ""},
		"unknown both": {"x", "y"},
	} {
		status, _, text := e.taskMemory(tc.task, tc.repo)
		if status < 400 || status >= 500 {
			t.Errorf("get with %s = %d %s, want a 4xx", name, status, text)
		}
		if status, text := e.saveTaskMemory(tc.task, tc.repo, "plan.md", "x"); status < 400 || status >= 500 {
			t.Errorf("save with %s = %d %s, want a 4xx", name, status, text)
		}
	}
	// A refused save stores nothing.
	if _, got, _ := e.taskMemory(task, e.repo); strings.Contains(got.file("plan.md"), "x\n") && got.file("plan.md") == "x" {
		t.Errorf("a refused save changed the plan")
	}
	for _, body := range []string{`{`, `{"task_id": 1}`, `{"task_id":"a","repository_id":"b","file":"plan.md","content":5}`} {
		if status, text := postJSON(t, e.srv.URL+"/api/save_task_memory", body); status != 400 {
			t.Errorf("save %q = %d %s, want 400", body, status, text)
		}
	}
}

func TestLearnAPI_SaveTaskMemoryRefusesEveryNameThatIsNotAMemoryFile(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Names")
	for _, name := range []string{"", "PLAN.md", "Plan.md", "plan", "plan.md ", " plan.md", "../plan.md", "./plan.md", "/etc/passwd", "plan.md/../x", "notes.md", "progress.md\x00", "progress.md.bak", "sub/progress.md"} {
		status, text := e.saveTaskMemory(task, e.repo, name, "NEW")
		if status != 400 || !strings.Contains(text, "INVALID_INPUT") {
			t.Errorf("save as %q = %d %s, want 400 INVALID_INPUT", name, status, text)
		}
	}
	_, got, _ := e.taskMemory(task, e.repo)
	for _, f := range got.Files {
		if f.Content == "NEW" {
			t.Errorf("a refused name stored content in %s", f.Name)
		}
	}
	if len(got.Files) != 2 {
		t.Errorf("files = %+v, want only plan.md and progress.md", got.Files)
	}
}

func TestLearnAPI_SaveTaskMemoryWithVeryLargeContent(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Large")
	for _, size := range []int{1 << 20, 8 << 20, 40 << 20} {
		content := strings.Repeat("line of progress\n", size/17)
		status, text := e.saveTaskMemory(task, e.repo, "progress.md", content)
		switch status {
		case 200:
			_, got, _ := e.taskMemory(task, e.repo)
			if got.file("progress.md") != content {
				t.Errorf("%d bytes: saved with 200 but read back %d bytes", size, len(got.file("progress.md")))
			}
		case 400, 413:
			// refused cleanly: nothing changed
			if _, got, _ := e.taskMemory(task, e.repo); got.file("progress.md") == content {
				t.Errorf("%d bytes refused (%d) but stored", size, status)
			}
		default:
			t.Errorf("%d bytes = %d %.200s, want 200, 400 or 413", size, status, text)
		}
	}
}

func TestLearnAPI_SaveTaskMemoryEmptyContentAndUnicodeRoundTrip(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Roundtrip")
	uni := "# Progress\r\n\r\n## Done\r\n\r\n- ação — 日本語 🚀 `code`\r\n"
	if status, text := e.saveTaskMemory(task, e.repo, "progress.md", uni); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	if _, got, _ := e.taskMemory(task, e.repo); got.file("progress.md") != uni {
		t.Errorf("read back %q", got.file("progress.md"))
	}
	if status, text := e.saveTaskMemory(task, e.repo, "plan.md", ""); status != 200 {
		t.Fatalf("an empty plan = %d %s", status, text)
	}
	_, got, _ := e.taskMemory(task, e.repo)
	if got.file("plan.md") != "" {
		t.Errorf("an empty save reads back %q", got.file("plan.md"))
	}
}

func TestLearnAPI_TaskMemoryOfATaskInAnotherProjectIsNotReadableThroughThisRepository(t *testing.T) {
	e := newShellE2E(t)
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", e.repoDir)
	if err != nil {
		t.Skipf("a second project cannot be made here: %v", err)
	}
	foreign, err := e.plan.CreateTicket(e.ctx, other.ID, "Foreign", "", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	// The task belongs to another project than the repository: a memory for
	// that pair must not exist.
	if status, _, text := e.taskMemory(foreign.ID, e.repo); status == 200 {
		t.Errorf("a task of project %s read through a repository of project %s: %s", other.ID, e.project, text)
	}
}

// ---- B: a fake agent host ----

func TestLearnHost_TwoSessionsOfOneTaskShareOneFolderAndTheLaterTurnEndWins(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Two sessions")
	a, specA := e.startRaw(task)
	b, specB := e.startRaw(task)
	if memoryDirOf(t, specA) != memoryDirOf(t, specB) {
		t.Fatal("the sessions have different folders")
	}
	// Different files: both survive.
	writeMemoryOnDisk(t, specA, "plan.md", "# Plan\n\n## Steps\n\n- [done] from A\n")
	writeMemoryOnDisk(t, specB, "progress.md", progressWith("- from B"))
	for _, id := range []string{a, b} {
		if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
			t.Fatal(err)
		}
	}
	_, got, _ := e.taskMemory(task, e.repo)
	if !strings.Contains(got.file("plan.md"), "from A") || !strings.Contains(got.file("progress.md"), "- from B") {
		t.Fatalf("a session's file was lost: plan=%q progress=%q", got.file("plan.md"), got.file("progress.md"))
	}
	// One file, written one after the other: the later turn end wins.
	writeMemoryOnDisk(t, specA, "progress.md", progressWith("- A again"))
	if _, err := e.orch.TurnEnded(e.ctx, a, false); err != nil {
		t.Fatal(err)
	}
	writeMemoryOnDisk(t, specB, "progress.md", progressWith("- B last"))
	if _, err := e.orch.TurnEnded(e.ctx, b, false); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := e.taskMemory(task, e.repo); !strings.Contains(got.file("progress.md"), "B last") || strings.Contains(got.file("progress.md"), "A again") {
		t.Errorf("progress.md = %q, want the later turn end's", got.file("progress.md"))
	}
	// A third session starts from the shared state.
	_, specC := e.startRaw(task)
	if raw, _ := os.ReadFile(filepath.Join(memoryDirOf(t, specC), "progress.md")); !strings.Contains(string(raw), "B last") {
		t.Errorf("a third session starts with %q", raw)
	}
}

func TestLearnHost_ThePersonEditsBetweenTurnsAndTheSessionSeesItAfterItsNextTurnEnd(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Person edits")
	id, spec := e.startRaw(task)
	writeMemoryOnDisk(t, spec, "progress.md", progressWith("- agent turn 1"))
	if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
		t.Fatal(err)
	}
	// The person rewrites progress.md and plan.md between turns.
	if status, text := e.saveTaskMemory(task, e.repo, "progress.md", progressWith("- the person was here")); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	if status, text := e.saveTaskMemory(task, e.repo, "plan.md", "# Plan\n\n- [todo] by the person\n"); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{"progress.md": "the person was here", "plan.md": "by the person"} {
		if raw, _ := os.ReadFile(filepath.Join(memoryDirOf(t, spec), file)); !strings.Contains(string(raw), want) {
			t.Errorf("%s on disk = %q, want the person's edit", file, raw)
		}
	}
	if _, got, _ := e.taskMemory(task, e.repo); !strings.Contains(got.file("progress.md"), "the person was here") {
		t.Errorf("the store lost the person's edit: %q", got.file("progress.md"))
	}
	// The agent writes again: that is read back over the person's text.
	writeMemoryOnDisk(t, spec, "progress.md", progressWith("- agent turn 2"))
	if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := e.taskMemory(task, e.repo); !strings.Contains(got.file("progress.md"), "agent turn 2") {
		t.Errorf("the agent's later write was dropped: %q", got.file("progress.md"))
	}
}

func TestLearnHost_ARestartMidTaskKeepsWhatBothSidesWrote(t *testing.T) {
	e := newShellE2EWith(t, "data.db")
	task := e.task("Restart both")
	_, spec := e.startRaw(task)
	writeMemoryOnDisk(t, spec, "progress.md", progressWith("- unsynced agent write"))
	// The person edits the other file in the store; neither is synced yet.
	if status, text := e.saveTaskMemory(task, e.repo, "plan.md", "# Plan\n\n- [doing] person plan\n"); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	e.restart()
	_, spec2 := e.startRaw(task)
	dir := memoryDirOf(t, spec2)
	if raw, _ := os.ReadFile(filepath.Join(dir, "plan.md")); !strings.Contains(string(raw), "person plan") {
		t.Errorf("plan.md after restart = %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "progress.md")); !strings.Contains(string(raw), "unsynced agent write") {
		t.Errorf("progress.md after restart = %q", raw)
	}
	_, got, _ := e.taskMemory(task, e.repo)
	if !strings.Contains(got.file("plan.md"), "person plan") || !strings.Contains(got.file("progress.md"), "unsynced agent write") {
		t.Errorf("the store after restart: plan=%q progress=%q", got.file("plan.md"), got.file("progress.md"))
	}
}

func TestLearnHost_ADeletedMemoryFileIsDeliveredAgainAtTheNextTurnEnd(t *testing.T) {
	e := newShellE2E(t)
	task := e.task("Deleted")
	id, spec := e.startRaw(task)
	writeMemoryOnDisk(t, spec, "progress.md", progressWith("- kept"))
	if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
		t.Fatal(err)
	}
	dir := memoryDirOf(t, spec)
	for _, f := range []string{"plan.md", "progress.md"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.orch.TurnEnded(e.ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "progress.md")); err != nil || !strings.Contains(string(raw), "- kept") {
		t.Errorf("progress.md after deletion = %q, %v", raw, err)
	}
	if _, got, _ := e.taskMemory(task, e.repo); !strings.Contains(got.file("progress.md"), "- kept") {
		t.Errorf("deleting the delivery copy emptied the store: %q", got.file("progress.md"))
	}
}

// -- skill activation: Write, MultiEdit, relative paths, bad rules, many skills

func TestLearnHost_APathRuleIsActivatedByWriteAndMultiEditAndRelativePathsAndOnlyOnce(t *testing.T) {
	for _, tool := range []string{"Write", "MultiEdit", "Edit", "Read"} {
		t.Run(tool, func(t *testing.T) {
			e := newShellE2E(t)
			e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
			s := e.start(e.task("Touch " + tool))
			if out := e.touch(s, tool, "web/new/created.templ"); !strings.Contains(out, "never raw html") {
				t.Fatalf("%s of a matching file = %s", tool, out)
			}
			if out := e.touch(s, "Write", "web/other.templ"); out != "" {
				t.Errorf("a second touch by another tool = %s", out)
			}
		})
	}
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	s := e.start(e.task("Relative"))
	out := runHook(t, s, e.srv.URL, "PostToolUse", skillTouchMatcher, map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"file_path": "web/rel/x.templ"}})
	if !strings.Contains(out, "never raw html") {
		t.Errorf("a path relative to the worktree = %s", out)
	}
}

func TestLearnHost_APathOutsideTheWorktreeOrWithoutAFileNeverActivates(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	s := e.start(e.task("Outside"))
	for name, in := range map[string]map[string]any{
		"outside absolute": {"tool_name": "Read", "tool_input": map[string]any{"file_path": "/elsewhere/web/x.templ"}},
		"dotdot":           {"tool_name": "Read", "tool_input": map[string]any{"file_path": filepath.Join(s.worktree, "..", "web", "x.templ")}},
		"no path":          {"tool_name": "Read", "tool_input": map[string]any{}},
		"empty path":       {"tool_name": "Write", "tool_input": map[string]any{"file_path": ""}},
	} {
		if out := runHook(t, s, e.srv.URL, "PostToolUse", skillTouchMatcher, in); out != "" {
			t.Errorf("%s = %s, want nothing", name, out)
		}
	}
	// The sessions's own first real touch still works afterwards.
	if out := e.touch(s, "Edit", "web/x.templ"); !strings.Contains(out, "never raw html") {
		t.Errorf("a refused touch used up the activation: %s", out)
	}
}

func TestLearnHost_TwoSkillsOnOneFileLoadTogetherAndABadRuleNeverBreaksTheGoodOne(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"templ-style/SKILL.md", templSkill)
	e.saveSkillFile(skillPrefix+"templ-tests/SKILL.md", "---\nname: templ-tests\ndescription: Test templ.\nmetadata:\n  activation_paths: \"web/**\"\n---\n\nTest every component.\n")
	e.saveSkillFile(skillPrefix+"broken/SKILL.md", "---\nname: broken\ndescription: Bad rule.\nmetadata:\n  activation_paths: \"web/[x\"\n---\n\nBROKEN-BODY\n")
	e.saveSkillFile(skillPrefix+"notaskill/other.md", "---\nname: other\nmetadata:\n  activation_paths: \"web/**\"\n---\n\nSIDE-FILE-BODY\n")
	s := e.start(e.task("Many"))
	out := e.touch(s, "Edit", "web/page.templ")
	for _, want := range []string{"never raw html", "Test every component"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer lacks %q: %s", want, out)
		}
	}
	for _, bad := range []string{"BROKEN-BODY", "SIDE-FILE-BODY"} {
		if strings.Contains(out, bad) {
			t.Errorf("the answer holds %s: %s", bad, out)
		}
	}
}

func TestLearnHost_ATaskTypeRuleLoadsOnlyForThatModeAndNeverForTheDefaultMode(t *testing.T) {
	e := newShellE2E(t)
	e.saveSkillFile(skillPrefix+"review-checklist/SKILL.md", reviewSkill)
	holds := func(mode string) bool {
		_, spec, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Mode " + mode), Mode: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		for _, f := range spec.InstructionFiles {
			if strings.Contains(string(f.Content), "Check the diff against the spec.") && strings.HasPrefix(f.Path, "SKILL-") {
				return true
			}
		}
		return false
	}
	for mode, want := range map[string]bool{"review": true, "implement": false, "test": false, "": false} {
		if got := holds(mode); got != want {
			t.Errorf("mode %q holds the activated body = %v, want %v", mode, got, want)
		}
	}
}

// -- the declaration check: require_update on and off, a restart, a person edit

func TestLearnHost_ADeclarationAfterTheAgentUpdatedTheMemoryTwiceIsNeverAsked(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	// First refused; the agent then updates progress.md and declares again.
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Fatalf("first declaration: %v", err)
	}
	if err := os.WriteFile(progressOf(e, sess), []byte(progressWith("- now it is done")), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := e.callTool(sess.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Fatalf("second declaration after the update = %v, %v", out, err)
	}
	events, _ := e.sessions.ListEvents(sess.ID, 0)
	for _, ev := range events {
		if ev.Type == "task_memory_omitted" {
			t.Error("an omission was recorded though the agent did update progress.md")
		}
	}
}

func TestLearnHost_OnlyWhitespaceOrLineEndingChangesCountAsNoUpdate(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	raw, err := os.ReadFile(progressOf(e, sess))
	if err != nil {
		t.Fatal(err)
	}
	reflowed := strings.ReplaceAll(string(raw), "\n", "\r\n") + "\r\n\r\n"
	if err := os.WriteFile(progressOf(e, sess), []byte(reflowed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Errorf("a whitespace-only rewrite passed as an update: %v", err)
	}
}

func TestLearnHost_ADeclarationOnAnUpdatedPlanButUntouchedProgressIsStillAsked(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	plan := filepath.Join(filepath.Dir(progressOf(e, sess)), "plan.md")
	if err := os.WriteFile(plan, []byte("# Plan\n\n- [done] everything\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Errorf("a plan.md update stood in for progress.md: %v", err)
	}
}

func TestLearnHost_TwoSessionsOfATaskAreAskedAndRecordedIndependently(t *testing.T) {
	e := newShellE2E(t)
	fake := &fakeAgentSystem{retry: 3, gates: greenGates()}
	a := e.gatedSession(fake)
	b, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: a.TicketID, Mode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.callTool(a.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Fatalf("a first: %v", err)
	}
	// B has not been asked yet: its first declaration is refused too.
	if _, err := e.callTool(b.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Fatalf("b first: %v", err)
	}
	if out, err := e.callTool(a.ID, "declare_done", nil); err != nil || out["accepted"] != true {
		t.Fatalf("a second = %v, %v", out, err)
	}
	for _, id := range []string{a.ID, b.ID} {
		var omitted int
		events, _ := e.sessions.ListEvents(id, 0)
		for _, ev := range events {
			if ev.Type == "task_memory_omitted" {
				omitted++
			}
		}
		want := map[string]int{a.ID: 1, b.ID: 0}[id]
		if omitted != want {
			t.Errorf("session %s omitted events = %d, want %d", id, omitted, want)
		}
	}
}

// What the person edits through the API is not the session's update; a session
// that never wrote still has to be asked. (The check compares progress.md with
// the one the session started with, so the person's edit looks like an update.)
func TestLearnHost_APersonsEditOfProgressIsNotTheSessionsUpdate(t *testing.T) {
	e := newShellE2E(t)
	sess := e.gatedSession(&fakeAgentSystem{retry: 3, gates: greenGates()})
	if status, text := e.saveTaskMemory(sess.TicketID, e.repo, "progress.md", progressWith("- the person wrote this")); status != 200 {
		t.Fatalf("save = %d %s", status, text)
	}
	if _, err := e.callTool(sess.ID, "declare_done", nil); err == nil || !strings.Contains(err.Error(), "TASK_MEMORY_NOT_UPDATED") {
		t.Errorf("the session never wrote progress.md but the person's edit satisfied the check: %v", err)
	}
}
