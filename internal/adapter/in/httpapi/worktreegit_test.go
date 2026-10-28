package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeWorktreeGit answers err, or fixed answers, and keeps what it was asked.
type fakeWorktreeGit struct {
	ports.WorktreeGit
	err    error
	fetch  string
	pushed []string
}

func (f *fakeWorktreeGit) FetchRemote(_ context.Context, repositoryID string) (time.Time, error) {
	f.fetch = repositoryID
	return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), f.err
}

func TestHTTP_FetchRemote_AnswersWhenItFetched(t *testing.T) {
	f := &fakeWorktreeGit{}
	h := NewHandler(Services{WorktreeGit: f})
	code, body := doJSON(t, h, http.MethodPost, "/api/fetch_remote", `{"repository_id":"r1"}`)
	if code != http.StatusOK || f.fetch != "r1" || body["fetched_at"] != "2026-10-28T12:00:00Z" {
		t.Fatalf("fetch_remote = %d %v (asked %q)", code, body, f.fetch)
	}
	if code, body := doJSON(t, NewHandler(Services{}), http.MethodPost, "/api/fetch_remote", `{"repository_id":"r1"}`); code != http.StatusServiceUnavailable || body["code"] != "UNAVAILABLE" {
		t.Errorf("not configured: %d %v", code, body)
	}
}

// Each refusal of the worktree git state, fetch and push has its status, with
// the details the contract names.
func TestHTTP_WorktreeGit_EachRefusalIsCodedAtItsStatus(t *testing.T) {
	for code, status := range map[string]int{
		"INVALID_INPUT": 400, "WORKSPACE_NOT_FOUND": 404, "REPOSITORY_NOT_FOUND": 404, "TASK_NOT_FOUND": 404,
		"NO_REMOTE": 400, "GIT_CREDENTIAL_MISSING": 400, "GIT_CREDENTIAL_REJECTED": 502, "GIT_CREDENTIAL_STORE_LOCKED": 503,
		"FETCH_FAILED": 502, "PUSH_REJECTED": 409, "PUSH_STALE": 409, "PUSH_FAILED": 502, "WORKSPACE_MISSING": 409, "UNAVAILABLE": 503,
	} {
		state := &domain.GitState{WorkspaceID: "w1", Upstream: domain.UpstreamState{State: domain.UpstreamBehind}}
		f := &fakeWorktreeGit{err: domain.GitStateError(code, code+": because", state)}
		h := NewHandler(Services{WorktreeGit: f})
		got, body := doJSON(t, h, http.MethodPost, "/api/fetch_remote", `{"repository_id":"r1"}`)
		if got != status || body["code"] != code || body["error"] != code+": because" {
			t.Errorf("%s: %d %v, want %d", code, got, body, status)
		}
		details, _ := body["details"].(map[string]any)
		if st, _ := details["state"].(map[string]any); st["workspace_id"] != "w1" {
			t.Errorf("%s: details = %v, want the state", code, body["details"])
		}
	}
}

// A git state on the project feed is the state itself under the git_state
// key, with nothing of the routing around it.
func TestHTTP_Events_StreamsGitState(t *testing.T) {
	ref, remote, ahead, behind := "origin/agent/x", "origin", 2, 0
	state := &domain.GitState{
		WorkspaceID: "w1", RepositoryID: "r1", SessionID: "s1", Branch: "agent/x", Head: "abc",
		Upstream: domain.UpstreamState{State: domain.UpstreamAhead, Ref: &ref, Remote: &remote, Ahead: &ahead, Behind: &behind},
		ReadAt:   time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
	}
	ch := make(chan ports.ProjectChange, 1)
	ch <- ports.ProjectChange{GitState: &ports.GitStateChange{GitState: state, ProjectID: "p1"}}
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: feedOnly{ch: ch}}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events?project_id=p1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		data, ok := strings.CutPrefix(scan.Text(), "data: ")
		if !ok {
			continue
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &got); err != nil || len(got) != 1 || got["git_state"] == nil {
			t.Fatalf("event = %s, want only a git_state key", data)
		}
		want, _ := json.Marshal(state)
		if string(got["git_state"]) != string(want) {
			t.Fatalf("git_state = %s\nwant      %s", got["git_state"], want)
		}
		return
	}
	t.Fatalf("no event: %v", scan.Err())
}
