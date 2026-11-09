//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The Branches list and the after-turn check, over HTTP with the real server,
// real git and the real shim (contract "Harness server ↔ Web UI — branch
// requests and the Branches list" §1, §2, §4 and §5; ADR 0070).

func (g *branchGate) branches() []domain.SessionBranch { return g.branchesOf(g.id) }

func (g *branchGate) branchesOf(sessionID string) []domain.SessionBranch {
	g.t.Helper()
	res, err := http.Get(g.srv.URL + "/api/session_branches?session_id=" + sessionID)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Branches []domain.SessionBranch `json:"branches"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		g.t.Fatal(err)
	}
	return out.Branches
}

func rowOf(list []domain.SessionBranch, name string) *domain.SessionBranch {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// startWatch is startBranchGateWith with the person's branches, and a bare
// origin when asked, made in the repository before the session starts.
func startWatch(t *testing.T, db string, remote bool) (*branchGate, string) {
	t.Helper()
	bin := buildServer(t)
	e := newShellE2EWith(t, db)
	e.orch.SessionBranchGateURL = func(id string) string { return e.srv.URL + "/api/sessions/" + id + "/branch_gate" }
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = e.repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("branch", "other")
	run("branch", "to-delete")
	run("branch", "to-rename")
	bare := ""
	if remote {
		bare = filepath.Join(t.TempDir(), "origin.git")
		run("init", "-q", "--bare", bare)
		run("remote", "add", "origin", bare)
		run("push", "-q", "origin", "main")
	}
	g := &branchGate{shellE2E: e}
	g.shimSession = e.startWithShims(e.task("Branch gate"), bin)
	return g, bare
}

// endTurnOf ends the session's turn, which runs the after-turn check.
func (g *branchGate) endTurnOf(id string) {
	g.t.Helper()
	if _, err := g.orch.TurnEnded(g.ctx, id, false); err != nil {
		g.t.Fatal(err)
	}
}

// findings are the inbox notifications about branches nothing explains.
func (g *branchGate) findings() []*domain.Notification {
	g.t.Helper()
	page, err := g.shellE2E.notes.ListNotifications(g.ctx, ports.NotificationQuery{Limit: 200})
	if err != nil {
		g.t.Fatal(err)
	}
	var out []*domain.Notification
	for _, n := range page.Notifications {
		if n.Kind == domain.NotificationKind("branch_finding") {
			out = append(out, n)
		}
	}
	return out
}

// approved runs one approved branch action through the asking hook and the shim.
func (g *branchGate) approved(command string) {
	g.t.Helper()
	hook := g.askHook(command, "")
	req := g.pending(1)[0]
	if code, body := g.answer(req.ID, "approve", ""); code != http.StatusOK {
		g.t.Fatalf("answer: %d %s", code, body)
	}
	if a := answered(g.t, hook); a.decision() != "allow" {
		g.t.Fatalf("hook: %+v", a)
	}
	if r := g.git(command); r.code != 0 {
		g.t.Fatalf("%s: %+v", command, r)
	}
}

func (g *branchGate) sessionBranch() string {
	return strings.TrimSpace(g.git("git rev-parse --abbrev-ref HEAD").stdout)
}

// writeRef makes a branch the way a git library does, around the gate.
func (g *branchGate) writeRef(name string) {
	g.t.Helper()
	head := strings.TrimSpace(g.git("git rev-parse HEAD").stdout)
	common := strings.TrimSpace(g.git("git rev-parse --git-common-dir").stdout)
	if !filepath.IsAbs(common) {
		common = filepath.Join(g.worktree, common)
	}
	if err := os.WriteFile(filepath.Join(common, "refs", "heads", name), []byte(head+"\n"), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

func TestAnApprovedCreateShowsWithItsCountsAndEachStateOnTheList(t *testing.T) {
	g, _ := startWatch(t, ":memory:", false)
	session := g.sessionBranch()

	if list := g.branches(); len(list) != 1 || list[0].Name != session || list[0].State != domain.SessionBranchSession || !list[0].IsSessionBranch || list[0].FlaggedAt != nil {
		t.Fatalf("a session with only its branch: %+v", list)
	}

	// approved create, with a commit on it
	g.approved("git checkout -b feature-x")
	g.setup("-c user.name=n -c user.email=n@x commit --allow-empty -m on-feature-x", "checkout "+session)
	// denied
	hook := g.askHook("git checkout -b nope", "")
	req := g.pending(1)[0]
	if code, body := g.answer(req.ID, "deny", "no"); code != http.StatusOK {
		t.Fatalf("answer: %d %s", code, body)
	}
	answered(t, hook)
	// deleted with approval
	g.approved("git branch -D to-delete")

	g.endTurnOf(g.id)
	eventually(t, "the list after the turn", func() error {
		list := g.branches()
		x := rowOf(list, "feature-x")
		if x == nil || x.State != domain.SessionBranchApproved || x.Base == nil || x.Base.Ref != session || x.Base.Ahead != 1 || x.Base.Behind != 0 {
			return fmt.Errorf("feature-x = %+v", x)
		}
		return nil
	})
	list := g.branches()
	if list[0].Name != session {
		t.Errorf("the session branch is not first: %+v", list)
	}
	if d := rowOf(list, "nope"); d == nil || d.State != domain.SessionBranchDenied || d.Base != nil {
		t.Errorf("denied = %+v", d)
	}
	if d := rowOf(list, "to-delete"); d == nil || d.State != domain.SessionBranchDeleted || d.Base != nil || len(d.RequestIDs) != 1 {
		t.Errorf("deleted = %+v", d)
	}
	if o := rowOf(list, "other"); o != nil {
		t.Errorf("a branch that existed before the session is listed: %+v", o)
	}
	if got := g.findings(); len(got) != 0 {
		t.Errorf("approved actions were flagged: %+v", got)
	}
}

func TestTheListOfATaskGroupsItsSessionsAndIsTheSameAfterARestart(t *testing.T) {
	g, _ := startWatch(t, "watch.db", false)
	session := g.sessionBranch()
	g.approved("git checkout -b feature-x")
	g.setup("checkout " + session)
	g.writeRef("sneaky")
	g.endTurnOf(g.id)
	eventually(t, "the flag", func() error {
		if x := rowOf(g.branches(), "sneaky"); x == nil || x.State != domain.SessionBranchUnapproved {
			return fmt.Errorf("sneaky = %+v", x)
		}
		return nil
	})
	before, _ := json.Marshal(g.branches())

	g.restartGate()
	after, _ := json.Marshal(g.branches())
	if string(before) != string(after) {
		t.Errorf("after a restart:\n%s\nbefore:\n%s", after, before)
	}

	res, err := http.Get(g.srv.URL + "/api/list_session_branches?task_id=" + g.sessions.Get(g.id).TicketID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Sessions []domain.SessionBranches `json:"sessions"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || len(out.Sessions) != 1 || out.Sessions[0].SessionID != g.id {
		t.Fatalf("task list: %+v, %v", out, err)
	}
	if rowOf(out.Sessions[0].Branches, "sneaky") == nil || rowOf(out.Sessions[0].Branches, "feature-x") == nil {
		t.Errorf("task list rows: %+v", out.Sessions[0].Branches)
	}
}

func TestABranchMadeAroundTheGateIsFlaggedAfterTheTurnWithOneInboxEntry(t *testing.T) {
	g, _ := startWatch(t, ":memory:", false)
	g.writeRef("sneaky")
	if x := rowOf(g.branches(), "sneaky"); x != nil {
		t.Fatalf("flagged before the turn ended: %+v", x)
	}
	g.endTurnOf(g.id)
	eventually(t, "the flag", func() error {
		x := rowOf(g.branches(), "sneaky")
		if x == nil || x.State != domain.SessionBranchUnapproved || x.FlaggedAt == nil {
			return fmt.Errorf("sneaky = %+v", x)
		}
		return nil
	})
	got := g.findings()
	if len(got) != 1 || got[0].Level != domain.NotificationAction || !strings.Contains(got[0].Title+got[0].Body, "sneaky") ||
		!strings.Contains(got[0].Body, g.id) || !strings.Contains(got[0].Body, g.repo) {
		t.Fatalf("inbox = %+v", got)
	}
	// The same finding is not posted again, and branches from before the session are not flagged.
	g.endTurnOf(g.id)
	g.endTurnOf(g.id)
	eventually(t, "the checks to run", func() error {
		if len(g.findings()) != 1 {
			return fmt.Errorf("%d inbox entries", len(g.findings()))
		}
		return nil
	})
	if o := rowOf(g.branches(), "other"); o != nil {
		t.Errorf("a branch from before the session: %+v", o)
	}
}

func TestOverlappingTurnsGiveOneUnattributedEntryAndNoFalseFlag(t *testing.T) {
	g, _ := startWatch(t, ":memory:", false)
	second := g.start(g.task("Second"))
	g.writeRef("mystery")
	g.endTurnOf(g.id)
	g.endTurnOf(second.id)
	eventually(t, "the inbox entry", func() error {
		if len(g.findings()) == 0 {
			return fmt.Errorf("no entry")
		}
		return nil
	})
	got := g.findings()
	if len(got) != 1 || !strings.Contains(got[0].Title+got[0].Body, "mystery") || !strings.Contains(got[0].Body, g.id) || !strings.Contains(got[0].Body, second.id) || !strings.Contains(got[0].Body, g.repo) {
		t.Fatalf("inbox = %+v", got)
	}
	for _, id := range []string{g.id, second.id} {
		if x := rowOf(g.branchesOf(id), "mystery"); x != nil {
			t.Errorf("a false flag on %s: %+v", id, x)
		}
	}
	g.endTurnOf(g.id)
	g.endTurnOf(second.id)
	eventually(t, "no repeat", func() error {
		if len(g.findings()) != 1 {
			return fmt.Errorf("%d entries", len(g.findings()))
		}
		return nil
	})
}

func TestAPushAroundTheGateIsFlaggedAndAFetchIsNot(t *testing.T) {
	g, bare := startWatch(t, ":memory:", true)
	session := g.sessionBranch()

	// another clone pushes a branch, and the harness fetches it
	other := filepath.Join(t.TempDir(), "other")
	for _, args := range [][]string{{"clone", "-q", bare, other}, {"-C", other, "checkout", "-q", "-b", "theirs"}, {"-C", other, "-c", "user.name=n", "-c", "user.email=n@x", "commit", "-q", "--allow-empty", "-m", "t"}, {"-C", other, "push", "-q", "origin", "theirs"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if r := g.git("git fetch origin"); r.code != 0 {
		t.Fatalf("fetch: %+v", r)
	}
	g.endTurnOf(g.id)
	g.endTurnOf(g.id)
	if got := g.findings(); len(got) != 0 {
		t.Fatalf("a fetch was flagged: %+v", got)
	}

	// a push the gate never saw (the shim only gates what it classifies; raw git by path goes round it)
	g.setup("push -q origin " + session)
	g.endTurnOf(g.id)
	eventually(t, "the flag", func() error {
		x := rowOf(g.branches(), session)
		if x == nil || len(x.Pushed) != 1 || x.Pushed[0].By != domain.PushedByUnapproved || x.Pushed[0].Remote != "origin" || x.FlaggedAt == nil {
			return fmt.Errorf("%s = %+v", session, x)
		}
		return nil
	})
	if got := g.findings(); len(got) != 1 || !strings.Contains(got[0].Title, "push") {
		t.Errorf("inbox = %+v", got)
	}
}

// The person's Push goes through the server's push path, which records it once;
// the check takes the record as the explanation and the list says who pushed.
func TestThePersonsPushIsRecordedOnceFillsPushedAndFlagsNothing(t *testing.T) {
	g, _ := startWatch(t, ":memory:", true)
	session := g.sessionBranch()
	g.setup("-c user.name=n -c user.email=n@x commit --allow-empty -m work")
	head := strings.TrimSpace(g.git("git rev-parse HEAD").stdout)
	b, _ := json.Marshal(map[string]string{"workspace_id": g.sessions.Get(g.id).WorkspaceID, "expected_head": head})
	res, err := http.Post(g.srv.URL+"/api/push_branch", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	_, _ = io.Copy(&body, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("push: %d %s", res.StatusCode, body.String())
	}
	if got, err := g.pushes.PushesOf(g.ctx, g.id); err != nil || len(got) != 1 || got[0].By != domain.PushByPerson || got[0].Commit != head {
		t.Fatalf("recorded pushes = %+v, %v; want exactly one by the person", got, err)
	}
	g.endTurnOf(g.id)
	g.endTurnOf(g.id)
	eventually(t, "pushed by the person", func() error {
		x := rowOf(g.branches(), session)
		if x == nil || len(x.Pushed) != 1 || x.Pushed[0].By != domain.PushedByPerson || x.Pushed[0].Remote != "origin" || x.Pushed[0].RemoteBranch != session {
			return fmt.Errorf("%s = %+v", session, x)
		}
		return nil
	})
	time.Sleep(500 * time.Millisecond)
	if got := g.findings(); len(got) != 0 || g.branches()[0].FlaggedAt != nil {
		t.Errorf("the person's push was flagged: %+v", got)
	}
	if got, _ := g.pushes.PushesOf(g.ctx, g.id); len(got) != 1 {
		t.Errorf("recorded pushes after the checks = %d, want 1", len(got))
	}
}
