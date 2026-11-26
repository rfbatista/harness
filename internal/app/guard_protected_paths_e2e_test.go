//go:build !windows

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Guard — protected paths (ADR 0012): a gated session's write to a path its
// kept configuration protects is refused by the same hook and PATH shims as the
// architecture rules, and the refusal is recorded and announced.

const protectedMessage = "PROTECTED_PATH: check.sh matches the protected path check.sh"

// protectedSession starts a session of mode with the shims on its PATH, over the
// default protected paths.
func (e *shellE2E) protectedSession(bin string, mode domain.SessionMode, realURL bool) shimSession {
	e.t.Helper()
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	if realURL { // the shims call the test server; the hook tests rewrite the configured base
		e.orch.SessionArchitectureURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/architecture" }
	}
	return e.startWithShimsMode(e.task("Protected"), bin, mode)
}

func (e *shellE2E) ruleEvents() []map[string]any {
	e.t.Helper()
	var out struct {
		Events []map[string]any `json:"events"`
	}
	e.post("list_architecture_rule_events", map[string]any{"repository_id": e.repo}, &out)
	return out.Events
}

func TestAGatedSessionsWriteToAProtectedPathIsRefusedByTheHookAndByTheShellAndRecorded(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeImplement, false)
	feed, err := e.orch.FollowProject(e.ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}
	check := filepath.Join(a.worktree, "check.sh")
	write(t, check, "echo ok\n")
	workflow := filepath.Join(a.worktree, ".github", "workflows", "ci.yml")

	// The file tools, through the hook, by absolute and relative path.
	for _, tool := range []string{"Edit", "Write", "MultiEdit"} {
		out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput(tool, check))
		if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, protectedMessage) {
			t.Errorf("%s of check.sh = %s", tool, out)
		}
	}
	out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", ".github/workflows/ci.yml"))
	if !strings.Contains(out, "PROTECTED_PATH: .github/workflows/ci.yml matches the protected path .github/workflows/**") {
		t.Errorf("a path under a ** glob = %s", out)
	}

	// A redirect in a Bash command, through the hook.
	for _, cmd := range []string{"echo x > check.sh", "echo x >> check.sh", "cat <<EOF > .github/workflows/ci.yml\nx\nEOF"} {
		out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, bashInput(a.worktree, cmd))
		if !strings.Contains(out, "PROTECTED_PATH") {
			t.Errorf("%q through the Bash hook = %s", cmd, out)
		}
	}

	// A write to an unprotected path goes through, as does a command that only reads.
	for _, in := range []map[string]any{
		editInput("Write", filepath.Join(a.worktree, "main.go")),
		bashInput(a.worktree, "echo x > notes.txt"),
		bashInput(a.worktree, "cat check.sh"),
	} {
		if out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, in); out != "" {
			t.Errorf("an allowed action = %s", out)
		}
	}

	// The PATH shims, in a second session that calls the test server directly.
	hooksID := a.id
	a = e.protectedSession(bin, domain.SessionModeImplement, true)
	check = filepath.Join(a.worktree, "check.sh")
	write(t, check, "echo ok\n")
	workflow = filepath.Join(a.worktree, ".github", "workflows", "ci.yml")
	// sed -i, tee, cp, mv, rm and touch refuse, and the file is as it was.
	write(t, filepath.Join(a.worktree, "other.sh"), "other\n")
	for name, line := range map[string]string{
		"sed -i": "sed " + inPlace() + " 's/ok/bad/' check.sh",
		"tee":    "printf 'bad\\n' | tee check.sh",
		"cp":     "cp other.sh check.sh",
		"mv":     "mv other.sh check.sh",
		"rm":     "rm check.sh",
		"touch":  "touch .github/workflows/new.yml",
	} {
		r := a.inWorktree(t, line)
		if r.code != 1 || !strings.Contains(r.stderr, "PROTECTED_PATH") {
			t.Errorf("%s: %+v, want exit 1 with PROTECTED_PATH", name, r)
		}
	}
	if got := read(t, check); got != "echo ok\n" {
		t.Errorf("check.sh = %q after the refused writes", got)
	}
	if got := read(t, workflow); got != "<missing>" {
		t.Errorf("a refused touch created %s", workflow)
	}
	if r := a.inWorktree(t, "printf 'x\\n' | tee notes.txt"); r.code != 0 {
		t.Errorf("an unprotected tee: %+v", r)
	}

	// Every refusal is recorded with the session, and announced.
	perSession := map[string]int{}
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] != "protected_path" {
			continue
		}
		perSession[ev["session_id"].(string)]++
		if ev["outcome"] != "blocked" || ev["at_point"] != "before_tool" || ev["detail"] == "" {
			t.Errorf("event = %v", ev)
		}
	}
	if perSession[hooksID] != 7 || perSession[a.id] != 6 {
		t.Errorf("recorded refusals per session = %v, want 7 for the hook session and 6 for the shim session", perSession)
	}
	var announced bool
drain:
	for {
		select {
		case ch := <-feed:
			if ch.ArchitectureRuleEvent != nil && ch.ArchitectureRuleEvent.Event.RuleID == "protected_path" {
				announced = true
			}
		default:
			break drain
		}
	}
	if !announced {
		t.Error("no protected-path refusal reached the project feed")
	}
}

func TestASessionThatIsNotGatedMayWriteAProtectedPath(t *testing.T) {
	bin := buildServer(t)
	e := newShellE2E(t)
	a := e.protectedSession(bin, domain.SessionModeOperate, false)
	if out := runHook(t, a.runningSession, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", filepath.Join(a.worktree, "check.sh"))); out != "" {
		t.Errorf("a non-gated Edit of check.sh = %s", out)
	}
	e.orch.SessionArchitectureURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/architecture" }
	b := e.protectedSession(bin, domain.SessionModeOperate, true)
	write(t, filepath.Join(b.worktree, "check.sh"), "echo ok\n")
	if r := b.inWorktree(t, "printf 'new\\n' | tee check.sh"); r.code != 0 || read(t, filepath.Join(b.worktree, "check.sh")) != "new\n" {
		t.Errorf("a non-gated tee: %+v", r)
	}
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] == "protected_path" {
			t.Errorf("a non-gated write left an event: %v", ev)
		}
	}
}

func TestASessionScopeUnlocksOnlyTheGlobsItCoversForThatSessionAndSurvivesAResume(t *testing.T) {
	e := newShellE2E(t)
	e.orch.AgentSystem = &fakeAgentSystem{retry: 3, gates: greenGates()}
	ticket := e.task("Scoped")
	start := func(scope ...string) runningSession {
		return e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticket, Mode: "implement", Scope: scope})
	}
	hook := func(s runningSession, rel string) string {
		return runHook(t, s, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", filepath.Join(s.worktree, rel)))
	}
	scoped, sibling := start("check.sh"), start()

	if out := hook(scoped, "check.sh"); out != "" {
		t.Errorf("the scoped session's write of check.sh = %s", out)
	}
	if out := hook(sibling, "check.sh"); !strings.Contains(out, "PROTECTED_PATH") {
		t.Errorf("the sibling's write of check.sh = %s", out)
	}
	// A scope that covers only part of the protected globs unlocks only that part.
	if out := hook(scoped, ".github/workflows/ci.yml"); !strings.Contains(out, "PROTECTED_PATH") {
		t.Errorf("a protected path the scope does not cover = %s", out)
	}
	if got, _ := e.gateRuns.Scope(e.ctx, scoped.id); len(got) != 1 || got[0] != "check.sh" {
		t.Errorf("the session's record shows scope %v", got)
	}
	if got, _ := e.gateRuns.Scope(e.ctx, sibling.id); len(got) != 0 {
		t.Errorf("the sibling's record shows scope %v", got)
	}

	// The scope survives a resume, which takes the settings again.
	e.orch.Transcripts = transcriptsThatResume{}
	if _, err := e.orch.EndInteractive(e.ctx, scoped.id, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.orch.ResumeInteractive(e.ctx, ports.ResumeRequest{SessionID: scoped.id, RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if out := hook(scoped, "check.sh"); out != "" {
		t.Errorf("after a resume the scoped session's write of check.sh = %s", out)
	}
	if out := hook(scoped, ".github/workflows/ci.yml"); !strings.Contains(out, "PROTECTED_PATH") {
		t.Errorf("after a resume an uncovered path = %s", out)
	}

	// An invalid glob is refused at start.
	if _, _, err := e.orch.StartInteractive(e.ctx, ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticket, Mode: "implement", Scope: []string{"[x"}}); err == nil {
		t.Error("an invalid scope glob started a session")
	}
}

// Guard — the lockfile rule (ADR 0012): a lockfile changes only with its
// manifest, and a change with it is allowed and flagged with what it added.

func (e *shellE2E) gatedForLockfile(settings *fakeAgentSystem, mode string) runningSession {
	e.t.Helper()
	e.orch.AgentSystem = settings
	return e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: e.task("Lockfile"), Mode: mode})
}

const goSumAdded = "github.com/new/dep v1.2.3 h1:abc=\ngithub.com/new/dep v1.2.3/go.mod h1:def=\n"

func TestALockfileWrittenWithoutItsManifestIsRefusedAndWithItAllowedAndFlaggedWithTheNewDependencies(t *testing.T) {
	e := newShellE2E(t)
	a := e.gatedForLockfile(&fakeAgentSystem{retry: 3, gates: greenGates()}, "implement")
	feed, err := e.orch.FollowProject(e.ctx, e.project)
	if err != nil {
		t.Fatal(err)
	}
	sum := filepath.Join(a.worktree, "go.sum")
	pre := func(in map[string]any) string {
		return runHook(t, a, e.srv.URL, "PreToolUse", archPreMatcher, in)
	}

	// The lockfile alone, by a file tool and by a redirect.
	for _, in := range []map[string]any{editInput("Write", sum), bashInput(a.worktree, "echo x >> go.sum")} {
		if out := pre(in); !strings.Contains(out, "LOCKFILE_WITHOUT_MANIFEST") || !strings.Contains(out, "go.mod") {
			t.Errorf("a lockfile alone = %s", out)
		}
	}
	// A manifest is not a lockfile, and a write of it is allowed.
	if out := pre(editInput("Write", filepath.Join(a.worktree, "go.mod"))); out != "" {
		t.Errorf("a manifest write = %s", out)
	}

	// With the manifest changed in the worktree, the lockfile is allowed...
	write(t, filepath.Join(a.worktree, "go.mod"), "module x\n\nrequire github.com/new/dep v1.2.3\n")
	if out := pre(editInput("Write", sum)); out != "" {
		t.Fatalf("a lockfile with its manifest changed = %s", out)
	}
	// ...and once the edit landed, flagged with the dependencies it added.
	write(t, sum, goSumAdded)
	runHook(t, a, e.srv.URL, "PostToolUse", archPostMatcher, editInput("Write", sum))

	var flagged map[string]any
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] == "lockfile_flag" {
			flagged = ev
		}
	}
	if flagged == nil || flagged["session_id"] != a.id || flagged["outcome"] != "warned" || !strings.Contains(flagged["detail"].(string), "github.com/new/dep@v1.2.3") {
		t.Errorf("flag event = %v", flagged)
	}
	var refusals int
	for _, ev := range e.ruleEvents() {
		if ev["rule_id"] == "lockfile_without_manifest" && ev["outcome"] == "blocked" {
			refusals++
		}
	}
	if refusals != 2 {
		t.Errorf("recorded %d lockfile refusals, want 2", refusals)
	}
	var announced *ports.LockfileFlagChange
drain:
	for {
		select {
		case ch := <-feed:
			if ch.LockfileFlag != nil {
				announced = ch.LockfileFlag
			}
		default:
			break drain
		}
	}
	if announced == nil || announced.Lockfile != "go.sum" || announced.Manifest != "go.mod" || len(announced.Dependencies) != 1 || announced.Dependencies[0] != "github.com/new/dep@v1.2.3" {
		t.Errorf("feed event = %+v", announced)
	}
}

func TestWithTheLockfileRuleOffNothingIsRefusedOrFlaggedAndANonGatedSessionIsNeverChecked(t *testing.T) {
	e := newShellE2E(t)
	off := e.gatedForLockfile(&fakeAgentSystem{retry: 3, gates: greenGates(), lockfileOff: true}, "implement")
	sum := filepath.Join(off.worktree, "go.sum")
	if out := runHook(t, off, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", sum)); out != "" {
		t.Errorf("the rule off refused = %s", out)
	}
	write(t, filepath.Join(off.worktree, "go.mod"), "module x\n")
	write(t, sum, goSumAdded)
	runHook(t, off, e.srv.URL, "PostToolUse", archPostMatcher, editInput("Write", sum))

	plain := e.gatedForLockfile(&fakeAgentSystem{retry: 3, gates: greenGates()}, "operate")
	if out := runHook(t, plain, e.srv.URL, "PreToolUse", archPreMatcher, editInput("Write", filepath.Join(plain.worktree, "go.sum"))); out != "" {
		t.Errorf("a non-gated session was checked = %s", out)
	}
	for _, ev := range e.ruleEvents() {
		if strings.HasPrefix(ev["rule_id"].(string), "lockfile") {
			t.Errorf("an event with the rule off or no gate: %v", ev)
		}
	}
}
