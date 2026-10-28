package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeGitStates answers the states it holds for the task it is asked about.
type fakeGitStates struct {
	ports.WorktreeGit
	byTask map[string][]domain.GitState
	err    error
	asked  []string
}

func (f *fakeGitStates) TaskGitStates(_ context.Context, taskID string) ([]domain.GitState, error) {
	f.asked = append(f.asked, taskID)
	return f.byTask[taskID], f.err
}

func gitStatesOf(t *testing.T, seed map[string]any) []map[string]any {
	t.Helper()
	raw, ok := seed["git_states"].([]any)
	if !ok {
		t.Fatalf("git_states = %#v, want a list", seed["git_states"])
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

// The task page seeds the git state of every session worktree of the task,
// in the order the port answers them, so the chips need no second request.
func TestTaskPageSeedsTheGitStateOfItsSessionWorktrees(t *testing.T) {
	w := board()
	ahead, behind := 2, 0
	ref, remote := "origin/agent/feed", "origin"
	w.gitStates = &fakeGitStates{byTask: map[string][]domain.GitState{"t-feed": {
		{WorkspaceID: "w1", SessionID: "s1", Branch: "agent/feed", Upstream: domain.UpstreamState{State: domain.UpstreamAhead, Ref: &ref, Remote: &remote, Ahead: &ahead, Behind: &behind}, Uncommitted: domain.Uncommitted{Tracked: 3, Untracked: 2}},
		{WorkspaceID: "w2", SessionID: "s2", Gone: true, Upstream: domain.UpstreamState{State: domain.UpstreamNone}},
	}}}
	rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	states := gitStatesOf(t, seedOf(t, rec.Body.String(), "sessions-seed"))
	if len(states) != 2 || states[0]["session_id"] != "s1" || states[1]["session_id"] != "s2" || states[1]["gone"] != true {
		t.Fatalf("git_states = %v, want s1 then the gone s2", states)
	}
	up := states[0]["upstream"].(map[string]any)
	if up["state"] != "ahead" || up["ref"] != "origin/agent/feed" || up["ahead"] != float64(2) {
		t.Errorf("s1 upstream = %v", up)
	}
	if u := states[0]["uncommitted"].(map[string]any); u["tracked"] != float64(3) || u["untracked"] != float64(2) {
		t.Errorf("s1 uncommitted = %v", u)
	}
}

// With no reader, or a read that fails, the page still paints with no states:
// the chips are not worth the page.
func TestTaskPageSeedsNoGitStatesWhenTheyCannotBeRead(t *testing.T) {
	for name, w := range map[string]world{"no reader": board(), "a failing reader": func() world {
		w := board()
		w.gitStates = &fakeGitStates{err: errs.Newf("UNAVAILABLE", "git is away")}
		return w
	}()} {
		rec := get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		if states := gitStatesOf(t, seedOf(t, rec.Body.String(), "sessions-seed")); len(states) != 0 {
			t.Errorf("%s: git_states = %v, want none", name, states)
		}
	}
}
