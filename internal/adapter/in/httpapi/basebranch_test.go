package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// refusingBases refuses every base with err.
type refusingBases struct{ err error }

func (r refusingBases) PrepareBase(context.Context, string, string, string) error { return r.err }

// A start from a base the server refuses answers the contract's status, code,
// message and details, and the start made no session.
func TestHTTP_AStartFromARefusedBaseAnswersTheContractsRefusal(t *testing.T) {
	gone := &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.RemoteBranchNotFound, Message: "origin/feature-x is not on origin any more. Fetch from origin to refresh the list."},
		Details:         map[string]string{"base_branch": "origin/feature-x", "remote": "origin", "branch": "feature-x"},
	}
	missing := &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.BaseBranchNotFound, Message: "mian is not a branch of this repository."},
		Details:         map[string]string{"base_branch": "mian"},
	}
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		details map[string]any
	}{
		{"gone from origin", gone, 404, "REMOTE_BRANCH_NOT_FOUND", map[string]any{"base_branch": "origin/feature-x", "remote": "origin", "branch": "feature-x"}},
		{"not a branch", missing, 404, "BASE_BRANCH_NOT_FOUND", map[string]any{"base_branch": "mian"}},
		{"no origin", &domain.StructuredError{Code: domain.CodeNoRemote, Message: "no origin"}, 400, "NO_REMOTE", nil},
		{"no credential", domain.GitCredentialMissing("github.com", domain.GitHTTPSToken), 400, "GIT_CREDENTIAL_MISSING", map[string]any{"host": "github.com", "kind": string(domain.GitHTTPSToken)}},
		{"credential refused", domain.GitCredentialRejected("github.com", "c1", "bad"), 502, "GIT_CREDENTIAL_REJECTED", map[string]any{"host": "github.com", "credential_id": "c1"}},
		{"fetch failed", &domain.StructuredError{Code: domain.CodeFetchFailed, Message: "fetching origin/x failed: boom"}, 502, "FETCH_FAILED", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newSessionTestEnv(t)
			defer env.srv.Close()
			env.orch.UseBaseBranches(refusingBases{tc.err})
			resp, err := http.Post(env.srv.URL+"/api/start_interactive_session", "application/json",
				strings.NewReader(`{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","base_branch":"origin/feature-x"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct {
				Error   string         `json:"error"`
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if resp.StatusCode != tc.status || body.Code != tc.code {
				t.Fatalf("= %d %q, want %d %q", resp.StatusCode, body.Code, tc.status, tc.code)
			}
			for k, v := range tc.details {
				if body.Details[k] != v {
					t.Fatalf("details = %v, want %v", body.Details, tc.details)
				}
			}
		})
	}
}
