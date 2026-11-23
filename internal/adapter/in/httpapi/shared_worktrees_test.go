package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The wire side of joining a worktree (contract "Harness server ↔ Web UI —
// joining a worktree and the task worktree setting", §1, §2, §5).

// Get lets the stub provisioner answer a join like workspaces.Service.
func (s *stubProvisioner) Get(_ context.Context, id string) *domain.Workspace {
	for _, ws := range s.created {
		if ws.ID == id {
			return ws
		}
	}
	return nil
}

type fakeLister struct {
	ports.Orchestration
	view         *ports.TaskWorktreeChoices
	err          error
	task, repoID string
}

func (f *fakeLister) TaskWorktreeChoices(_ context.Context, task, repo string) (*ports.TaskWorktreeChoices, error) {
	f.task, f.repoID = task, repo
	return f.view, f.err
}

func TestHTTP_TaskWorktrees_AnswersThePickersListAsIs(t *testing.T) {
	f := &fakeLister{view: &ports.TaskWorktreeChoices{
		Policy: ports.TaskWorktreePolicy{TaskOverride: "project"},
		Worktrees: []ports.TaskWorktreeChoice{{
			WorktreeID: "ws1", Branch: "agent/work", OnDisk: true, CreatedBySessionID: "s1",
			Sessions: []ports.TaskWorktreeSession{
				{SessionID: "s1", Agent: "go-developer", Mode: "implement", Access: "write", Running: true},
				{SessionID: "s2", Agent: "reviewer", Mode: "review", Access: "read_only"},
			},
		}},
	}}
	code, body := doJSON(t, NewHandler(Services{Sessions: f}), http.MethodGet, "/api/task_worktrees?task_id=tk1&repository_id=r1", "")
	if code != http.StatusOK || f.task != "tk1" || f.repoID != "r1" {
		t.Fatalf("%d asked %q %q: %v", code, f.task, f.repoID, body)
	}
	policy := body["policy"].(map[string]any)
	if policy["task_override"] != "project" || policy["effective"] != false || policy["project"] != false {
		t.Errorf("policy = %v", policy)
	}
	if v, has := body["task_worktree_id"]; !has || v != nil || body["choice_needed"] != false {
		t.Errorf("task_worktree_id %v (present %v), choice_needed %v", v, has, body["choice_needed"])
	}
	w := body["worktrees"].([]any)[0].(map[string]any)
	if w["worktree_id"] != "ws1" || w["branch"] != "agent/work" || w["on_disk"] != true || w["created_by_session_id"] != "s1" {
		t.Errorf("worktree = %v", w)
	}
	sessions := w["sessions"].([]any)
	second := sessions[1].(map[string]any)
	if len(sessions) != 2 || second["access"] != "read_only" || second["running"] != false || second["agent"] != "reviewer" || second["mode"] != "review" {
		t.Errorf("sessions = %v", sessions)
	}
}

func TestHTTP_TaskWorktrees_RefusalsAndTheEmptyList(t *testing.T) {
	f := &fakeLister{err: &domain.StructuredError{Code: "INVALID_INPUT", Message: "task_id and repository_id are required"}}
	if code, body := doJSON(t, NewHandler(Services{Sessions: f}), http.MethodGet, "/api/task_worktrees", ""); code != 400 || body["code"] != "INVALID_INPUT" {
		t.Errorf("missing ids: %d %v", code, body)
	}
	if code, _ := doJSON(t, NewHandler(Services{}), http.MethodGet, "/api/task_worktrees?task_id=t&repository_id=r", ""); code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured: %d", code)
	}
	f = &fakeLister{view: &ports.TaskWorktreeChoices{Policy: ports.TaskWorktreePolicy{TaskOverride: "project"}, Worktrees: []ports.TaskWorktreeChoice{}}}
	code, body := doJSON(t, NewHandler(Services{Sessions: f}), http.MethodGet, "/api/task_worktrees?task_id=t&repository_id=r", "")
	if list, ok := body["worktrees"].([]any); code != 200 || !ok || len(list) != 0 || body["policy"] == nil {
		t.Errorf("empty repository: %d %v", code, body)
	}
}

func TestHTTP_StartInteractiveSession_OnAnExistingWorktree(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()

	code, body := startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"implement"}`)
	creator, _ := body["session"].(map[string]any)
	if code != http.StatusCreated || creator["access"] != "write" || creator["joined"] != false || creator["worktree_id"] == "" || creator["worktree_id"] != creator["workspace_id"] {
		t.Fatalf("a normal start: %d %v", code, body)
	}
	ws := creator["worktree_id"].(string)

	code, body = startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"review","worktree_id":"`+ws+`"}`)
	joined, _ := body["session"].(map[string]any)
	if code != http.StatusCreated || joined["joined"] != true || joined["access"] != "read_only" || joined["worktree_id"] != ws || joined["branch"] != creator["branch"] {
		t.Fatalf("a join: %d %v", code, body)
	}
	if got := len(env.prov.created); got != 1 {
		t.Errorf("%d worktrees created, want the one the creator cut", got)
	}
}

func TestHTTP_StartInteractiveSession_RefusesAJoinWithItsCodeAndASentence(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	_, body := startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"implement"}`)
	ws := body["session"].(map[string]any)["worktree_id"].(string)
	before := len(env.prov.created)

	for name, c := range map[string]struct {
		req    string
		status int
		code   string
	}{
		"unknown worktree":   {`"worktree_id":"nope"`, 404, "WORKTREE_NOT_FOUND"},
		"with a base branch": {`"worktree_id":"` + ws + `","base_branch":"main"`, 400, "WORKTREE_WITH_BASE_BRANCH"},
		"with a branch name": {`"worktree_id":"` + ws + `","branch_name":"mine"`, 400, "WORKTREE_WITH_BASE_BRANCH"},
		"an invalid access":  {`"worktree_id":"` + ws + `","access":"admin"`, 400, "ACCESS_INVALID"},
		"read-only, no join": {`"access":"read_only"`, 400, "ACCESS_INVALID"},
	} {
		code, out := startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","mode":"implement",`+c.req+`}`)
		msg, _ := out["error"].(string)
		if code != c.status || out["code"] != c.code || !strings.HasPrefix(msg, c.code+": ") || strings.TrimPrefix(msg, c.code+": ") == "" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if got := len(env.prov.created); got != before {
		t.Errorf("a refusal created a worktree: %d → %d", before, got)
	}
}
