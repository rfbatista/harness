//go:build !windows

package app

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The task worktrees lookup (contract "Harness server ↔ Agent sessions —
// architect worktree visibility", "The lookup"): what an architect's worktree
// read hook asks to learn which worktrees are its task's.

type taskWorktreesAnswer struct {
	Root      string `json:"worktrees_root"`
	Worktrees []struct {
		SessionID string `json:"session_id"`
		Dir       string `json:"dir"`
		Branch    string `json:"branch"`
		Agent     string `json:"agent"`
		Status    string `json:"status"`
	} `json:"worktrees"`
}

func (e *shellE2E) taskWorktrees(sessionID string) (int, taskWorktreesAnswer, string) {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/sessions/" + sessionID + "/task_worktrees")
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out taskWorktreesAnswer
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out, string(b)
}

func (e *shellE2E) architect(ticketID string) runningSession {
	e.t.Helper()
	return e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, TicketID: ticketID, Mode: "architect"})
}

func TestTheTaskWorktreesLookupListsTheWorktreesOfTheCallersTaskOnly(t *testing.T) {
	e := newShellE2E(t)
	mine, other := e.task("Mine"), e.task("Other")
	arch := e.architect(mine)
	delegate := e.start(mine)
	stranger := e.start(other)

	status, got, body := e.taskWorktrees(arch.id)
	if status != http.StatusOK {
		t.Fatalf("lookup = %d %s", status, body)
	}
	repo, err := e.cat.Projects.GetRepository(e.ctx, e.repo)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(e.cfg.Root, "worktrees", domain.RepositoryWorktreesDirName(repo))
	if got.Root != wantRoot {
		t.Errorf("worktrees_root = %q, want %q", got.Root, wantRoot)
	}
	dirs := map[string]string{}
	for _, w := range got.Worktrees {
		dirs[w.SessionID] = w.Dir
		if w.Branch == "" || w.Status == "" {
			t.Errorf("worktree %+v lacks its branch or status", w)
		}
	}
	if dirs[delegate.id] != delegate.worktree || dirs[arch.id] != arch.worktree {
		t.Errorf("worktrees = %v, want the architect's %s and the delegate's %s", dirs, arch.worktree, delegate.worktree)
	}
	if _, ok := dirs[stranger.id]; ok {
		t.Errorf("another task's worktree is listed: %v", dirs)
	}

	later := e.start(mine)
	if _, got, _ = e.taskWorktrees(arch.id); !listsSession(got, later.id) {
		t.Errorf("a delegate started later is not listed: %+v", got)
	}

	if _, err := e.orch.EndInteractive(e.ctx, delegate.id, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, got, _ = e.taskWorktrees(arch.id); !listsSession(got, delegate.id) {
		t.Errorf("an ended session's worktree is not listed: %+v", got)
	}
}

func listsSession(a taskWorktreesAnswer, id string) bool {
	for _, w := range a.Worktrees {
		if w.SessionID == id {
			return true
		}
	}
	return false
}

func TestTheTaskWorktreesLookupAnswersNoWorktreeToASessionWithNoTaskAndNotFoundToAnUnknownOne(t *testing.T) {
	e := newShellE2E(t)
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{orchestration.SettingSandbox: "off"}); err != nil {
		t.Fatal(err)
	}
	sess, err := e.orch.Start(e.ctx, orchestration.StartRequest{ProjectID: e.project, RepositoryID: e.repo, Task: "no task"})
	if err != nil {
		t.Fatal(err)
	}
	e.start(e.task("Busy"))
	status, got, body := e.taskWorktrees(sess.ID)
	if status != http.StatusOK || got.Root == "" || len(got.Worktrees) != 0 {
		t.Errorf("no-task lookup = %d %s", status, body)
	}
	if status, _, body := e.taskWorktrees("nope"); status != http.StatusNotFound {
		t.Errorf("unknown session = %d %s, want 404 SESSION_NOT_FOUND", status, body)
	}
}
