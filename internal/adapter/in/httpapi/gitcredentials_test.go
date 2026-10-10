package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// stubProjectFeed is a project catalog feed a test announces on.
type stubProjectFeed struct {
	ch chan ports.ProjectCatalogChange
}

func (f stubProjectFeed) FollowProjects(context.Context) (<-chan ports.ProjectCatalogChange, error) {
	return f.ch, nil
}

func gitCredentialsServer(t *testing.T) (*httptest.Server, *gitcredentialstest.Fake, stubProjectFeed) {
	t.Helper()
	fake := gitcredentialstest.New()
	feed := stubProjectFeed{ch: make(chan ports.ProjectCatalogChange, 4)}
	srv := httptest.NewServer(NewRouter(NewHandler(Services{GitCredentials: fake, ProjectFeed: feed})))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv, fake, feed
}

func credCall(t *testing.T, method, url, body string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// The templ pages seed what the port returns; the API answers the same
// values. Both must be the same bytes, locked or not.
func TestGitCredentialSeedsAreTheAPIBodies(t *testing.T) {
	srv, fake, _ := gitCredentialsServer(t)
	ctx := context.Background()
	project, repo := fake.AddRepository("", "widgets", "https://github.com/acme/widgets.git")
	if _, err := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "GitHub", Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_seedtoken1234"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.UpdateGitIdentity(ctx, "Ada <&>", "ada@example.com"); err != nil {
		t.Fatal(err)
	}

	list, _ := fake.ListGitCredentials(ctx)
	identity, _ := fake.GetGitIdentity(ctx)
	one, _ := fake.GetRepositoryCredential(ctx, repo)
	all, _ := fake.ListRepositoryCredentials(ctx, project)
	for _, c := range []struct {
		path string
		seed any
	}{
		{"/api/list_git_credentials", list},
		{"/api/get_git_identity", map[string]any{"identity": identity}},
		{"/api/get_repository_credential?repository_id=" + repo, one},
		{"/api/list_repository_credentials?project_id=" + project, map[string]any{"repositories": all}},
	} {
		resp, body := credCall(t, "GET", srv.URL+c.path, "")
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", c.path, resp.StatusCode, body)
		}
		seed, err := json.Marshal(c.seed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.TrimSpace(body), seed) {
			t.Fatalf("%s:\n api  %s\n seed %s", c.path, bytes.TrimSpace(body), seed)
		}
		if bytes.Contains(body, []byte("ghp_seedtoken1234")) || bytes.Contains(body, []byte("PRIVATE KEY")) {
			t.Fatalf("%s shows a secret: %s", c.path, body)
		}
	}

	fake.Lock()
	locked, _ := fake.ListGitCredentials(ctx)
	_, body := credCall(t, "GET", srv.URL+"/api/list_git_credentials", "")
	seed, _ := json.Marshal(locked)
	if !locked.Locked || !bytes.Equal(bytes.TrimSpace(body), seed) || !bytes.Contains(body, []byte(`"locked":true`)) {
		t.Fatalf("locked:\n api  %s\n seed %s", bytes.TrimSpace(body), seed)
	}
}

func TestGitCredentialRoutesAnswerTheContractsStatusCodes(t *testing.T) {
	srv, fake, _ := gitCredentialsServer(t)
	ctx := context.Background()
	_, repo := fake.AddRepository("", "widgets", "https://github.com/acme/widgets.git")
	c, err := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "GitHub", Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_statuscodes0001"})
	if err != nil {
		t.Fatal(err)
	}
	gitlab, _ := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "GitLab", Kind: domain.GitHTTPSToken, Host: "gitlab.com", Token: "glpat_statuscodes"})
	key, _ := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true})
	cases := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/api/list_git_credentials", "", 200, ""},
		{"POST", "/api/create_git_credential", `{"name":"x","kind":"https_token","host":"github.com","token":"t0k3n-new"}`, 200, ""},
		{"POST", "/api/create_git_credential", `{"name":"github","kind":"https_token","host":"github.com","token":"t"}`, 409, domain.CodeGitCredentialNameTaken},
		{"POST", "/api/create_git_credential", `{"name":"y","kind":"https_token","host":"github.com"}`, 400, "INVALID_INPUT"},
		{"POST", "/api/create_git_credential", `{"name":"y","kind":"ssh_key","host":"github.com","private_key":"nope"}`, 400, domain.CodeGitCredentialInvalidKey},
		{"POST", "/api/create_git_credential", `not json`, 400, ""},
		{"POST", "/api/update_git_credential", `{"name":"x"}`, 400, "INVALID_INPUT"},
		{"POST", "/api/update_git_credential", `{"credential_id":"missing","name":"x"}`, 404, domain.CodeGitCredentialNotFound},
		{"POST", "/api/update_git_credential", `{"credential_id":"` + c.ID + `","username":"octocat"}`, 200, ""},
		{"POST", "/api/test_git_credential", `{"credential_id":"missing"}`, 404, domain.CodeGitCredentialNotFound},
		{"POST", "/api/test_git_credential", `{"credential_id":"` + c.ID + `"}`, 200, ""},
		{"GET", "/api/get_git_identity", "", 200, ""},
		{"POST", "/api/update_git_identity", `{"name":"Ada","email":"nope"}`, 400, "INVALID_INPUT"},
		{"POST", "/api/update_git_identity", `{"name":"Ada","email":"ada@example.com"}`, 200, ""},
		{"GET", "/api/get_repository_credential", "", 400, "INVALID_INPUT"},
		{"GET", "/api/get_repository_credential?repository_id=missing", "", 404, "REPOSITORY_NOT_FOUND"},
		{"GET", "/api/list_repository_credentials", "", 400, "INVALID_INPUT"},
		{"GET", "/api/list_repository_credentials?project_id=missing", "", 404, "PROJECT_NOT_FOUND"},
		{"POST", "/api/set_repository_credential", `{"repository_id":"` + repo + `","credential_id":"` + gitlab.ID + `"}`, 400, domain.CodeGitCredentialHostMismatch},
		{"POST", "/api/set_repository_credential", `{"repository_id":"` + repo + `","credential_id":"` + key.ID + `"}`, 400, domain.CodeGitCredentialKindMismatch},
		{"POST", "/api/set_repository_credential", `{"repository_id":"` + repo + `","credential_id":"` + c.ID + `"}`, 200, ""},
		{"POST", "/api/set_repository_credential", `{"repository_id":"` + repo + `","credential_id":null}`, 200, ""},
		{"POST", "/api/delete_git_credential", `{"credential_id":"missing"}`, 404, domain.CodeGitCredentialNotFound},
		{"POST", "/api/delete_git_credential", `{"credential_id":"` + gitlab.ID + `"}`, 204, ""},
	}
	for _, tc := range cases {
		resp, body := credCall(t, tc.method, srv.URL+tc.path, tc.body)
		if resp.StatusCode != tc.status {
			t.Fatalf("%s %s %s: %d %s, want %d", tc.method, tc.path, tc.body, resp.StatusCode, body, tc.status)
		}
		if tc.code == "" {
			continue
		}
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(body, &e); err != nil || e.Code != tc.code || !strings.HasPrefix(e.Error, tc.code) {
			t.Fatalf("%s %s: body %s, want code %s leading the message", tc.method, tc.path, body, tc.code)
		}
	}

	fake.Lock()
	resp, body := credCall(t, "POST", srv.URL+"/api/create_git_credential", `{"name":"z","kind":"https_token","host":"github.com","token":"t0k3n-locked"}`)
	if resp.StatusCode != 503 || !strings.Contains(string(body), domain.CodeGitCredentialStoreLocked) {
		t.Fatalf("create while locked: %d %s", resp.StatusCode, body)
	}
}

// /api/project_events carries projects and credentials on one stream.
func TestProjectEventsCarryGitCredentialChanges(t *testing.T) {
	srv, fake, feed := gitCredentialsServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/project_events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	next := func() map[string]json.RawMessage {
		t.Helper()
		for lines.Scan() {
			data, ok := strings.CutPrefix(lines.Text(), "data: ")
			if !ok {
				continue
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal([]byte(data), &m); err != nil {
				t.Fatal(err)
			}
			return m
		}
		t.Fatalf("stream ended: %v", lines.Err())
		return nil
	}

	c, err := fake.CreateGitCredential(context.Background(), ports.CreateGitCredential{Name: "GitHub", Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_feedsecret01"})
	if err != nil {
		t.Fatal(err)
	}
	ev := next()
	var got domain.GitCredential
	if err := json.Unmarshal(ev["git_credential"], &got); err != nil || got.ID != c.ID || len(ev) != 1 {
		t.Fatalf("create event %v", ev)
	}
	if strings.Contains(string(ev["git_credential"]), "ghp_feedsecret01") {
		t.Fatal("the feed shows the token")
	}

	feed.ch <- ports.ProjectCatalogChange{Project: &domain.Project{ID: "p1", Name: "widgets"}}
	if ev := next(); ev["project"] == nil {
		t.Fatalf("project event %v", ev)
	}

	if _, err := fake.UpdateGitIdentity(context.Background(), "Ada", "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if ev := next(); string(ev["git_identity"]) != `{"name":"Ada","email":"ada@example.com"}` {
		t.Fatalf("identity event %v", ev)
	}

	if err := fake.DeleteGitCredential(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if ev := next(); string(ev["git_credential"]) != `{"id":"`+c.ID+`"}` || string(ev["deleted"]) != "true" {
		t.Fatalf("delete event %v", ev)
	}
}
