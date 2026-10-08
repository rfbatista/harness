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

// fakePullRequests answers what the test scripted and keeps what it was asked.
type fakePullRequests struct {
	view    *ports.PullRequestView
	created *domain.PullRequest
	err     error
	gotID   string
	gotReq  ports.CreatePullRequest
}

func (f *fakePullRequests) PullRequest(_ context.Context, id string) (*ports.PullRequestView, error) {
	f.gotID = id
	return f.view, f.err
}

func (f *fakePullRequests) CreatePullRequest(_ context.Context, req ports.CreatePullRequest) (*domain.PullRequest, error) {
	f.gotReq = req
	return f.created, f.err
}

var samplePR = &domain.PullRequest{WorkspaceID: "w1", RepositoryID: "r1", ProjectID: "p1", Number: 12,
	URL: "https://github.com/acme/widgets/pull/12", Title: "T", HeadBranch: "agent/x", BaseBranch: "main",
	State: domain.PullRequestOpen, CreatedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}

func doJSON(t *testing.T, h *Handler, method, target, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	NewRouter(h).ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: body %q: %v", method, target, rec.Body, err)
	}
	return rec.Code, out
}

func TestHTTP_PullRequest_AnswersTheDraftAndThePR(t *testing.T) {
	svc := &fakePullRequests{view: &ports.PullRequestView{
		Draft: domain.PullRequestDraft{WorkspaceID: "w1", RepositoryID: "r1", HeadBranch: "agent/x", BaseBranch: "main", Title: "T",
			Ahead: 2, Remote: &domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}, Blockers: []string{}},
	}}
	h := NewHandler(Services{PullRequests: svc})

	code, body := doJSON(t, h, http.MethodGet, "/api/pull_request?workspace_id=w1", "")
	if code != http.StatusOK || svc.gotID != "w1" {
		t.Fatalf("%d asked %q: %v", code, svc.gotID, body)
	}
	if pr, has := body["pull_request"]; !has || pr != nil {
		t.Fatalf("pull_request = %v, want present and null", body["pull_request"])
	}
	d := body["draft"].(map[string]any)
	if d["head_branch"] != "agent/x" || d["ahead"] != float64(2) || d["remote"].(map[string]any)["owner"] != "acme" {
		t.Fatalf("draft = %v", d)
	}
	if b, ok := d["blockers"].([]any); !ok || len(b) != 0 {
		t.Fatalf("blockers = %v, want []", d["blockers"])
	}

	svc.view.PullRequest = samplePR
	_, body = doJSON(t, h, http.MethodGet, "/api/pull_request?workspace_id=w1", "")
	pr := body["pull_request"].(map[string]any)
	if pr["number"] != float64(12) || pr["state"] != "open" || pr["created_at"] != "2026-10-07T12:00:00Z" {
		t.Fatalf("pull_request = %v", pr)
	}
	if _, has := pr["project_id"]; has {
		t.Fatalf("project_id on the wire: %v", pr)
	}
}

func TestHTTP_PullRequest_RequiresAWorkspace(t *testing.T) {
	h := NewHandler(Services{PullRequests: &fakePullRequests{}})
	code, body := doJSON(t, h, http.MethodGet, "/api/pull_request", "")
	if code != http.StatusBadRequest || body["code"] != "INVALID_INPUT" || !strings.HasPrefix(body["error"].(string), "INVALID_INPUT") {
		t.Fatalf("%d %v", code, body)
	}
}

func TestHTTP_CreatePullRequest(t *testing.T) {
	svc := &fakePullRequests{created: samplePR}
	h := NewHandler(Services{PullRequests: svc})
	code, body := doJSON(t, h, http.MethodPost, "/api/create_pull_request",
		`{"workspace_id":"w1","title":"T","body":"B","base_branch":"release","draft":true}`)
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	want := ports.CreatePullRequest{WorkspaceID: "w1", Title: "T", Body: "B", BaseBranch: "release", Draft: true}
	if svc.gotReq != want {
		t.Fatalf("asked %+v", svc.gotReq)
	}
	if pr := body["pull_request"].(map[string]any); pr["url"] != samplePR.URL {
		t.Fatalf("pull_request = %v", pr)
	}
	if code, _ := doJSON(t, h, http.MethodPost, "/api/create_pull_request", `{"workspace_id":"w1"`); code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", code)
	}
}

// Every code of the contract at its status, the message as the service wrote it.
func TestHTTP_CreatePullRequest_Refusals(t *testing.T) {
	for code, status := range map[string]int{
		"INVALID_INPUT":              http.StatusBadRequest,
		"WORKSPACE_NOT_FOUND":        http.StatusNotFound,
		domain.CodePRNoRemote:        http.StatusBadRequest,
		domain.CodePRNoCommits:       http.StatusConflict,
		domain.CodePRBaseNotFound:    http.StatusBadRequest,
		domain.CodePRAlreadyExists:   http.StatusConflict,
		domain.CodePRToolUnavailable: http.StatusServiceUnavailable,
		domain.CodePRPushFailed:      http.StatusBadGateway,
		domain.CodePRCreateFailed:    http.StatusBadGateway,
	} {
		h := NewHandler(Services{PullRequests: &fakePullRequests{err: domain.PullRequestError(code, "why")}})
		got, body := doJSON(t, h, http.MethodPost, "/api/create_pull_request", `{"workspace_id":"w1","title":"T"}`)
		if got != status || body["code"] != code || body["error"] != code+": why" {
			t.Errorf("%s = %d %v, want %d", code, got, body, status)
		}
	}
}

func TestHTTP_CreatePullRequest_AlreadyExistsCarriesThePR(t *testing.T) {
	err := &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.CodePRAlreadyExists, Message: "PR_ALREADY_EXISTS: open"},
		Details:         map[string]any{"pull_request": samplePR},
	}
	h := NewHandler(Services{PullRequests: &fakePullRequests{err: err}})
	code, body := doJSON(t, h, http.MethodPost, "/api/create_pull_request", `{"workspace_id":"w1","title":"T"}`)
	pr, _ := body["details"].(map[string]any)["pull_request"].(map[string]any)
	if code != http.StatusConflict || pr == nil || pr["number"] != float64(12) {
		t.Fatalf("%d %v", code, body)
	}
}

func TestHTTP_PullRequests_NotConfigured(t *testing.T) {
	h := NewHandler(Services{})
	if code, _ := doJSON(t, h, http.MethodGet, "/api/pull_request?workspace_id=w1", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("GET = %d", code)
	}
	if code, _ := doJSON(t, h, http.MethodPost, "/api/create_pull_request", `{}`); code != http.StatusServiceUnavailable {
		t.Fatalf("POST = %d", code)
	}
}

func TestHTTP_Events_StreamsPullRequests(t *testing.T) {
	ch := make(chan ports.ProjectChange, 1)
	ch <- ports.ProjectChange{PullRequest: samplePR}
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
		var m map[string]any
		_ = json.Unmarshal([]byte(data), &m)
		pr, _ := m["pull_request"].(map[string]any)
		if pr == nil || pr["number"] != float64(12) || len(m) != 1 {
			t.Fatalf("event = %s", data)
		}
		return
	}
	t.Fatalf("no event: %v", scan.Err())
}
