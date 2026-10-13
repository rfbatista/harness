package githubapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var widgets = domain.GitRemote{Host: "github.com", Owner: "acme", Repo: "widgets"}

const token domain.Secret = "ghp_apitoken0001"

const prJSON = `{"number":12,"html_url":"https://github.com/acme/widgets/pull/12","title":"Add PRs","state":"open",
"draft":true,"merged_at":null,"created_at":"2026-10-07T12:00:00Z","head":{"ref":"agent/x"},"base":{"ref":"main"}}`

// fakeGitHub answers with status and body, and records the request.
type fakeGitHub struct {
	status int
	body   string
	method string
	path   string
	auth   string
	sent   map[string]any
}

func serve(t *testing.T, f *fakeGitHub) *Host {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.method, f.path, f.auth = r.Method, r.URL.RequestURI(), r.Header.Get("Authorization")
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &f.sent)
		}
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(srv.Close)
	return &Host{BaseURL: srv.URL}
}

func TestOpenSendsThePullRequestWithTheToken(t *testing.T) {
	f := &fakeGitHub{status: 201, body: prJSON}
	h := serve(t, f)
	got, err := h.Open(context.Background(), widgets, token, ports.OpenPullRequest{Head: "agent/x", Base: "main", Title: "Add PRs", Body: "Why\n- a", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	want := ports.HostPullRequest{Number: 12, URL: "https://github.com/acme/widgets/pull/12", Title: "Add PRs", Head: "agent/x",
		Base: "main", Draft: true, State: domain.PullRequestOpen, CreatedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	if *got != want {
		t.Fatalf("got %+v\nwant %+v", *got, want)
	}
	if f.method != "POST" || f.path != "/repos/acme/widgets/pulls" || f.auth != "Bearer "+token.Reveal() {
		t.Fatalf("request %s %s auth %q", f.method, f.path, f.auth)
	}
	if f.sent["head"] != "agent/x" || f.sent["base"] != "main" || f.sent["body"] != "Why\n- a" || f.sent["draft"] != true {
		t.Fatalf("sent %v", f.sent)
	}
}

func TestOpenRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
		check  func(error) bool
	}{
		"exists": {422, `{"message":"Validation Failed","errors":[{"message":"A pull request already exists for acme:agent/x."}]}`,
			func(err error) bool { return errors.Is(err, ports.ErrPullRequestExists) }},
		"invalid": {422, `{"message":"Validation Failed","errors":[{"field":"base","code":"invalid"}]}`,
			func(err error) bool {
				return errs.Code(err) == domain.CodePRCreateFailed && strings.Contains(err.Error(), "Validation Failed; base invalid")
			}},
		"bad token": {401, `{"message":"Bad credentials"}`,
			func(err error) bool {
				return errors.Is(err, ports.ErrCodeHostUnauthorized) && strings.Contains(err.Error(), "Bad credentials")
			}},
	} {
		h := serve(t, &fakeGitHub{status: c.status, body: c.body})
		_, err := h.Open(context.Background(), widgets, token, ports.OpenPullRequest{Head: "agent/x", Base: "main", Title: "T"})
		if !c.check(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFindOpenAndGet(t *testing.T) {
	f := &fakeGitHub{status: 200, body: "[" + prJSON + "]"}
	h := serve(t, f)
	got, err := h.FindOpen(context.Background(), widgets, token, "agent/x")
	if err != nil || got == nil || got.Number != 12 {
		t.Fatalf("FindOpen = %+v, %v", got, err)
	}
	if f.path != "/repos/acme/widgets/pulls?head=acme%3Aagent%2Fx&per_page=1&state=open" {
		t.Fatalf("FindOpen asked %s", f.path)
	}
	f.body = "[]"
	if got, err := h.FindOpen(context.Background(), widgets, token, "agent/x"); err != nil || got != nil {
		t.Fatalf("FindOpen with none = %+v, %v", got, err)
	}
	f.body = strings.Replace(prJSON, `"merged_at":null`, `"merged_at":"2026-10-08T00:00:00Z"`, 1)
	got, err = h.Get(context.Background(), widgets, token, 12)
	if err != nil || got.State != domain.PullRequestMerged || f.path != "/repos/acme/widgets/pulls/12" {
		t.Fatalf("Get = %+v, %v (%s)", got, err, f.path)
	}
	f.status, f.body = 401, `{"message":"Bad credentials"}`
	if _, err := h.Get(context.Background(), widgets, token, 12); !errors.Is(err, ports.ErrCodeHostUnauthorized) {
		t.Fatalf("Get with a refused token: %v", err)
	}
}

func TestUser(t *testing.T) {
	f := &fakeGitHub{status: 200, body: `{"login":"octocat"}`}
	h := serve(t, f)
	ok, msg, checked, err := h.User(context.Background(), "github.com", token.Reveal())
	if err != nil || !ok || !strings.Contains(msg, "octocat") || !strings.HasSuffix(checked, "/user") || f.auth != "Bearer "+token.Reveal() {
		t.Fatalf("User = %v %q %q %v", ok, msg, checked, err)
	}
	f.status, f.body = 401, `{"message":"Bad credentials"}`
	ok, msg, _, err = h.User(context.Background(), "github.com", token.Reveal())
	if err != nil || ok || msg != "Bad credentials" {
		t.Fatalf("User with a refused token = %v %q %v", ok, msg, err)
	}
}

func TestTheAPIRootFollowsTheHost(t *testing.T) {
	h := New()
	if h.base("github.com") != "https://api.github.com" || h.base("git.example.com") != "https://git.example.com/api/v3" {
		t.Fatalf("bases %s %s", h.base("github.com"), h.base("git.example.com"))
	}
}
