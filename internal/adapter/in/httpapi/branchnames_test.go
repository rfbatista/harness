package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

func startWith(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/api/start_interactive_session", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHTTP_StartInteractiveSession_WithABranchName(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	f := &fakeBranchNames{check: &domain.BranchNameCheck{Valid: true, Available: true}}
	env.orch.UseBranchNames(f)

	code, body := startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","branch_name":" my/work "}`)
	if code != http.StatusCreated || body["session"].(map[string]any)["branch"] != "my/work" || f.name != "my/work" {
		t.Fatalf("%d %v (checked %q)", code, body, f.name)
	}

	f.check = &domain.BranchNameCheck{Valid: true, Code: domain.BranchExists, Reason: "the branch feat already exists", Suggestion: "feat-2"}
	code, body = startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","branch_name":"feat"}`)
	d, _ := body["details"].(map[string]any)
	if code != 409 || body["code"] != "BRANCH_EXISTS" || d["name"] != "feat" || d["suggestion"] != "feat-2" ||
		!strings.HasPrefix(body["error"].(string), "BRANCH_EXISTS") {
		t.Fatalf("exists: %d %v", code, body)
	}

	f.check = &domain.BranchNameCheck{Code: domain.BranchNameInvalid, Reason: "a branch name cannot contain spaces"}
	code, body = startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1","branch_name":"a b"}`)
	d, _ = body["details"].(map[string]any)
	if code != 400 || body["code"] != "BRANCH_NAME_INVALID" || d["name"] != "a b" || d["reason"] == "" {
		t.Fatalf("invalid: %d %v", code, body)
	}

	code, body = startWith(t, env.srv.URL, `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1"}`)
	if code != http.StatusCreated || !strings.HasPrefix(body["session"].(map[string]any)["branch"].(string), "agent/ship-the-thing-") {
		t.Fatalf("without a name: %d %v", code, body)
	}
}
