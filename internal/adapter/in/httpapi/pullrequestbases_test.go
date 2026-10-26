package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

type fakeBases struct {
	bases     *domain.PullRequestBases
	err       error
	got       string
	refreshed int
}

func (f *fakeBases) PullRequestBases(_ context.Context, id string) (*domain.PullRequestBases, error) {
	f.got = id
	return f.bases, f.err
}

func (f *fakeBases) RefreshPullRequestBases(_ context.Context, id string) (*domain.PullRequestBases, error) {
	f.got = id
	f.refreshed++
	return f.bases, f.err
}

func TestHTTP_PullRequestBases_AnswersOriginsBranches(t *testing.T) {
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	f := &fakeBases{bases: &domain.PullRequestBases{WorkspaceID: "w1", Branches: []string{"develop", "main"}, Default: "main", FetchedAt: &at}}
	h := NewHandler(Services{PullRequestBases: f})

	code, body := doJSON(t, h, http.MethodGet, "/api/pull_request_bases?workspace_id=w1", "")
	branches, _ := body["branches"].([]any)
	if code != 200 || f.got != "w1" || f.refreshed != 0 || len(branches) != 2 || branches[0] != "develop" ||
		body["default"] != "main" || body["fetched_at"] != "2026-10-26T09:00:00Z" || body["workspace_id"] != "w1" {
		t.Fatalf("%d %v (refreshed %d)", code, body, f.refreshed)
	}

	code, body = doJSON(t, h, http.MethodPost, "/api/refresh_pull_request_bases", `{"workspace_id":"w1"}`)
	if code != 200 || f.refreshed != 1 || f.got != "w1" || body["default"] != "main" {
		t.Fatalf("refresh: %d %v (refreshed %d)", code, body, f.refreshed)
	}

	f.bases = &domain.PullRequestBases{WorkspaceID: "w1", Branches: []string{}, Default: "main"}
	if _, body = doJSON(t, h, http.MethodGet, "/api/pull_request_bases?workspace_id=w1", ""); body["fetched_at"] != nil || body["branches"] == nil {
		t.Fatalf("an unknown fetch time is null and the list is []: %v", body)
	}
}

func TestHTTP_PullRequestBases_EachRefusalIsCodedAtItsStatus(t *testing.T) {
	for code, status := range map[string]int{
		"INVALID_INPUT": 400, "WORKSPACE_NOT_FOUND": 404, "PR_NO_REMOTE": 400,
		"GIT_CREDENTIAL_MISSING": 400, "GIT_CREDENTIAL_REJECTED": 502, "GIT_CREDENTIAL_STORE_LOCKED": 503, "PR_FETCH_FAILED": 502,
	} {
		f := &fakeBases{err: &domain.StructuredError{Code: code, Message: code + ": because"}}
		h := NewHandler(Services{PullRequestBases: f})
		got, body := doJSON(t, h, http.MethodPost, "/api/refresh_pull_request_bases", `{"workspace_id":"w1"}`)
		if got != status || body["code"] != code || body["error"] != code+": because" {
			t.Errorf("%s: %d %v, want %d", code, got, body, status)
		}
	}
	h := NewHandler(Services{PullRequestBases: &fakeBases{}})
	if got, body := doJSON(t, h, http.MethodGet, "/api/pull_request_bases", ""); got != 400 || body["code"] != "INVALID_INPUT" {
		t.Errorf("GET without a workspace: %d %v", got, body)
	}
	if got, body := doJSON(t, h, http.MethodPost, "/api/refresh_pull_request_bases", `{}`); got != 400 || body["code"] != "INVALID_INPUT" {
		t.Errorf("POST without a workspace: %d %v", got, body)
	}
	if got, _ := doJSON(t, NewHandler(Services{}), http.MethodGet, "/api/pull_request_bases?workspace_id=w", ""); got != 503 {
		t.Errorf("unconfigured: %d", got)
	}
}
