package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"
)

func TestParseGitRemoteReadsTheForms(t *testing.T) {
	cases := map[string]struct {
		url  string
		want GitRemote
		ok   bool
	}{
		"scp ssh":         {"git@github.com:acme/widgets.git", GitRemote{"github.com", "acme", "widgets"}, true},
		"scp no .git":     {"git@github.com:acme/widgets", GitRemote{"github.com", "acme", "widgets"}, true},
		"ssh url":         {"ssh://git@github.com/acme/widgets.git", GitRemote{"github.com", "acme", "widgets"}, true},
		"ssh url port":    {"ssh://git@github.com:22/acme/widgets.git", GitRemote{"github.com", "acme", "widgets"}, true},
		"https":           {"https://github.com/acme/widgets.git", GitRemote{"github.com", "acme", "widgets"}, true},
		"https slash":     {"https://github.com/acme/widgets/", GitRemote{"github.com", "acme", "widgets"}, true},
		"https with user": {"https://bob@github.com/acme/widgets", GitRemote{"github.com", "acme", "widgets"}, true},
		"other host":      {"git@gitlab.com:acme/widgets.git", GitRemote{"gitlab.com", "acme", "widgets"}, true},
		"host is lowered": {"https://GitHub.com/acme/widgets", GitRemote{"github.com", "acme", "widgets"}, true},
		"empty":           {"", GitRemote{}, false},
		"local path":      {"/srv/git/widgets.git", GitRemote{}, false},
		"file url":        {"file:///srv/git/widgets.git", GitRemote{}, false},
		"too deep":        {"https://github.com/acme/widgets/tree/main", GitRemote{}, false},
		"owner only":      {"https://github.com/acme", GitRemote{}, false},
		"spaces trimmed":  {"  git@github.com:acme/widgets.git\n", GitRemote{"github.com", "acme", "widgets"}, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := ParseGitRemote(c.url)
			if ok != c.ok || got != c.want {
				t.Fatalf("ParseGitRemote(%q) = %+v, %v; want %+v, %v", c.url, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestGitRemoteIsGitHubOnlyForGithubDotCom(t *testing.T) {
	if !(GitRemote{Host: "github.com", Owner: "a", Repo: "b"}).IsGitHub() {
		t.Fatal("github.com is GitHub")
	}
	if (GitRemote{Host: "gitlab.com", Owner: "a", Repo: "b"}).IsGitHub() {
		t.Fatal("gitlab.com is not GitHub")
	}
}

func TestBranchFromRefStripsRemoteAndRefPrefixes(t *testing.T) {
	cases := map[string]string{
		"main":                    "main",
		"origin/main":             "main",
		"refs/heads/feature/x":    "feature/x",
		"refs/remotes/origin/dev": "dev",
		"origin/agent/foo-12":     "agent/foo-12",
		"  main ":                 "main",
		"":                        "",
	}
	for in, want := range cases {
		if got := BranchFromRef(in); got != want {
			t.Errorf("BranchFromRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPullRequestClosedCoversClosedAndMerged(t *testing.T) {
	for state, want := range map[PullRequestState]bool{PullRequestOpen: false, PullRequestClosed: true, PullRequestMerged: true} {
		if got := (PullRequest{State: state}).Closed(); got != want {
			t.Errorf("Closed() for %s = %v, want %v", state, got, want)
		}
	}
}

// The wire shape is the contract's exactly: no project_id, no updated_at.
func TestPullRequestJSONIsTheContractShape(t *testing.T) {
	pr := PullRequest{
		WorkspaceID: "w1", RepositoryID: "r1", ProjectID: "p1", Number: 7, URL: "https://github.com/a/b/pull/7",
		Title: "T", HeadBranch: "agent/x", BaseBranch: "main", Draft: true, State: PullRequestOpen,
		CreatedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Now(),
	}
	b, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	want := []string{"workspace_id", "repository_id", "number", "url", "title", "head_branch", "base_branch", "draft", "state", "created_at"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want exactly %v", got, want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	if got["created_at"] != "2026-10-07T12:00:00Z" {
		t.Errorf("created_at = %v", got["created_at"])
	}
}

func TestPullRequestDraftJSONHasNullRemoteAndEmptyBlockers(t *testing.T) {
	b, _ := json.Marshal(PullRequestDraft{Blockers: []string{}})
	s := string(b)
	for _, frag := range []string{`"remote":null`, `"blockers":[]`, `"uncommitted_changes":0`, `"ahead":0`} {
		if !strings.Contains(s, frag) {
			t.Errorf("%s lacks %s", s, frag)
		}
	}
}

func TestPullRequestErrorMessageStartsWithItsCode(t *testing.T) {
	err := PullRequestError(CodePRNoRemote, "the repository has no origin remote")
	if errs.Code(err) != CodePRNoRemote {
		t.Fatalf("code = %q", errs.Code(err))
	}
	se := err.(*StructuredError)
	if !strings.HasPrefix(se.Message, CodePRNoRemote+": ") {
		t.Fatalf("message %q does not start with the code", se.Message)
	}
}
