package httpapi

import (
	"context"
	"net/http"
	"testing"

	"operators-mcp/internal/domain"
)

type fakeBranchNames struct {
	check            *domain.BranchNameCheck
	err              error
	repo, name, task string
}

func (f *fakeBranchNames) CheckBranchName(_ context.Context, repo, name, task string) (*domain.BranchNameCheck, error) {
	f.repo, f.name, f.task = repo, name, task
	return f.check, f.err
}

func TestHTTP_CheckBranchName_AnswersTheCheckAsIs(t *testing.T) {
	f := &fakeBranchNames{check: &domain.BranchNameCheck{Name: "feat", Generated: "agent/x-1234abcd", Valid: true,
		Code: domain.BranchExists, Reason: "the branch feat already exists", Suggestion: "feat-2"}}
	code, body := doJSON(t, NewHandler(Services{BranchNames: f}), http.MethodGet,
		"/api/check_branch_name?repository_id=r1&name=feat&ticket_id=t1&base_branch=main", "")
	if code != http.StatusOK || f.repo != "r1" || f.name != "feat" || f.task != "t1" {
		t.Fatalf("%d asked %+v: %v", code, f, body)
	}
	want := map[string]any{"name": "feat", "generated": "agent/x-1234abcd", "valid": true, "available": false,
		"code": "BRANCH_EXISTS", "reason": "the branch feat already exists", "suggestion": "feat-2"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
}

func TestHTTP_CheckBranchName_Refusals(t *testing.T) {
	f := &fakeBranchNames{}
	if code, body := doJSON(t, NewHandler(Services{BranchNames: f}), http.MethodGet, "/api/check_branch_name?name=x", ""); code != 400 || body["code"] != "INVALID_INPUT" {
		t.Errorf("missing repository: %d %v", code, body)
	}
	f.err = &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	if code, body := doJSON(t, NewHandler(Services{BranchNames: f}), http.MethodGet, "/api/check_branch_name?repository_id=nope", ""); code != 404 || body["code"] != "REPOSITORY_NOT_FOUND" {
		t.Errorf("unknown repository: %d %v", code, body)
	}
	if code, _ := doJSON(t, NewHandler(Services{}), http.MethodGet, "/api/check_branch_name?repository_id=r", ""); code != 503 {
		t.Errorf("unconfigured: %d", code)
	}
}
