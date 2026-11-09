//go:build !windows

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// The person's Push of a branch on a session's Branches list, over HTTP with
// the real server, real git and bare remotes (contract "Harness server ↔ Web
// UI — branch requests and the Branches list" §2 push_branch, §3; ADR 0046 as
// amended by ADR 0071).

// pushBranch posts one push_branch body and returns the status and the answer.
func (g *branchGate) postPushBranch(fields map[string]string) (int, map[string]json.RawMessage) {
	g.t.Helper()
	fields["workspace_id"] = g.sessions.Get(g.id).WorkspaceID
	b, _ := json.Marshal(fields)
	res, err := http.Post(g.srv.URL+"/api/push_branch", "application/json", strings.NewReader(string(b)))
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		g.t.Fatalf("answer %s: %v", raw, err)
	}
	return res.StatusCode, out
}

func codeOf(body map[string]json.RawMessage) string {
	var s string
	_ = json.Unmarshal(body["code"], &s)
	return s
}

// commitOn makes a commit on branch in the session's worktree and comes back.
func (g *branchGate) commitOn(branch, back string) string {
	g.t.Helper()
	g.setup("checkout -q "+branch, "-c user.name=n -c user.email=n@x commit -q --allow-empty -m on-"+branch)
	head := strings.TrimSpace(g.git("git rev-parse HEAD").stdout)
	g.setup("checkout -q " + back)
	return head
}

func TestAnApprovedBranchIsPushedToTheRemoteThePersonPicksAndRecordedOnItsRow(t *testing.T) {
	g, _ := startWatch(t, ":memory:", true)
	mirror := g.withRemote("mirror")
	session := g.sessionBranch()
	g.approved("git checkout -b feature-x")
	g.setup("checkout -q " + session)
	head := g.commitOn("feature-x", session)

	code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": "feature-x", "remote": "mirror", "remote_branch": "review/x"})
	if code != http.StatusOK {
		t.Fatalf("push: %d %s", code, body)
	}
	if got := remoteHas(mirror, "review/x"); got != head {
		t.Errorf("the mirror holds %q, want %s", got, head)
	}
	var row domain.SessionBranch
	if err := json.Unmarshal(body["branch"], &row); err != nil || row.Name != "feature-x" || len(row.Pushed) != 1 ||
		row.Pushed[0].Remote != "mirror" || row.Pushed[0].RemoteBranch != "review/x" || row.Pushed[0].By != domain.PushedByPerson {
		t.Errorf("branch = %s (%v)", body["branch"], err)
	}
	if _, ok := body["state"]; !ok {
		t.Errorf("no state in %v", body)
	}
	if got, err := g.pushes.PushesOf(g.ctx, g.id); err != nil || len(got) != 1 || got[0].By != domain.PushByPerson {
		t.Errorf("recorded = %+v, %v; want one by the person", got, err)
	}
	g.endTurnOf(g.id)
	if got := g.findings(); len(got) != 0 {
		t.Errorf("the person's push was flagged: %+v", got)
	}
}

func TestTheSessionBranchGoesToAnotherRemoteAndTheOldRequestShapeStillPushesToOrigin(t *testing.T) {
	g, origin := startWatch(t, ":memory:", true)
	mirror := g.withRemote("mirror")
	session := g.sessionBranch()
	g.setup("-c user.name=n -c user.email=n@x commit -q --allow-empty -m work")
	head := g.head()

	if code, body := g.postPushBranch(map[string]string{"expected_head": head, "remote": "mirror"}); code != http.StatusOK || remoteHas(mirror, session) != head {
		t.Fatalf("to the mirror: %d %s; holds %q", code, body, remoteHas(mirror, session))
	}
	g.setup("-c user.name=n -c user.email=n@x commit -q --allow-empty -m more")
	head = g.head()
	code, body := g.postPushBranch(map[string]string{"expected_head": head})
	if code != http.StatusOK || remoteHas(origin, session) != head {
		t.Fatalf("old shape: %d %s; origin holds %q", code, body, remoteHas(origin, session))
	}
	if _, ok := body["state"]; !ok {
		t.Errorf("no state in %v", body)
	}
}

func TestABranchOffTheSessionsListOrNotApprovedIsNotPushableAndNothingIsPushed(t *testing.T) {
	g, origin := startWatch(t, ":memory:", true)
	session := g.sessionBranch()
	g.approved("git branch -D to-delete")
	hook := g.askHook("git checkout -b nope", "")
	req := g.pending(1)[0]
	g.answer(req.ID, "deny", "no")
	answered(t, hook)
	g.writeRef("sneaky")
	g.endTurnOf(g.id)
	eventually(t, "sneaky is listed", func() error {
		if x := rowOf(g.branches(), "sneaky"); x == nil || x.State != domain.SessionBranchUnapproved {
			return fmt.Errorf("sneaky = %+v", x)
		}
		return nil
	})
	head := g.head()

	for _, branch := range []string{"nope", "to-delete", "sneaky", "other"} {
		code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": branch})
		if code != http.StatusConflict || codeOf(body) != "BRANCH_NOT_PUSHABLE" {
			t.Errorf("%s: %d %s", branch, code, body)
		}
		if remoteHas(origin, branch) != "" {
			t.Errorf("%s was pushed", branch)
		}
	}
	if code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": "no-such-branch"}); code != http.StatusNotFound || codeOf(body) != "BRANCH_NOT_FOUND" {
		t.Errorf("a branch that does not exist: %d %s", code, body)
	}
	if got, _ := g.pushes.PushesOf(g.ctx, g.id); len(got) != 0 {
		t.Errorf("recorded pushes = %+v", got)
	}
	_ = session
}

func TestAPushRefusesAnUnknownRemoteAStaleHeadAndANonFastForward(t *testing.T) {
	g, origin := startWatch(t, ":memory:", true)
	session := g.sessionBranch()
	g.approved("git checkout -b feature-x")
	head := g.commitOn("feature-x", session)

	if code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": "feature-x", "remote": "nowhere"}); code != http.StatusBadRequest || codeOf(body) != "REMOTE_NOT_FOUND" {
		t.Errorf("unknown remote: %d %s", code, body)
	}
	if code, body := g.postPushBranch(map[string]string{"expected_head": g.head(), "branch": "feature-x"}); code != http.StatusConflict || codeOf(body) != "PUSH_STALE" {
		t.Errorf("a head that is not the branch's: %d %s", code, body)
	}
	if code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": "feature-x"}); code != http.StatusOK {
		t.Fatalf("first push: %d %s", code, body)
	}
	// The remote branch moves on elsewhere; the branch then diverges.
	other := t.TempDir()
	for _, a := range [][]string{
		{"clone", "-q", origin, other},
		{"-C", other, "checkout", "-q", "-B", "feature-x", "origin/feature-x"},
		{"-C", other, "-c", "user.name=x", "-c", "user.email=x@x", "commit", "-q", "--allow-empty", "-m", "remote"},
		{"-C", other, "push", "-q", "origin", "HEAD:refs/heads/feature-x"},
	} {
		if out, err := exec.Command("git", a...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	g.setup("checkout -q feature-x", "-c user.name=n -c user.email=n@x commit -q --allow-empty -m local")
	head = g.head()
	g.setup("checkout -q " + session)
	if code, body := g.postPushBranch(map[string]string{"expected_head": head, "branch": "feature-x"}); code != http.StatusConflict || codeOf(body) != "PUSH_REJECTED" {
		t.Errorf("non-fast-forward: %d %s", code, body)
	}
}

func TestARemoteWhoseHostHasNoStoredCredentialAnswersTheCredentialErrorNamingTheHost(t *testing.T) {
	g, _ := startWatch(t, ":memory:", true)
	g.setup("remote add hosted https://git.example.invalid/team/repo.git")
	code, body := g.postPushBranch(map[string]string{"expected_head": g.head(), "remote": "hosted"})
	if code == http.StatusOK || codeOf(body) != "GIT_CREDENTIAL_MISSING" || !strings.Contains(string(body["error"]), "git.example.invalid") {
		t.Errorf("no credential: %d %s", code, body)
	}
}

// remotesOf reads GET /api/repository_remotes.
func (g *branchGate) remotesOf(repo string) (int, []struct{ Name, URL string }, map[string]any) {
	g.t.Helper()
	res, err := http.Get(g.srv.URL + "/api/repository_remotes?repository_id=" + repo)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var ok struct {
		Remotes []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"remotes"`
	}
	_ = json.Unmarshal(raw, &ok)
	var anyBody map[string]any
	_ = json.Unmarshal(raw, &anyBody)
	out := make([]struct{ Name, URL string }, 0, len(ok.Remotes))
	for _, r := range ok.Remotes {
		out = append(out, struct{ Name, URL string }{r.Name, r.URL})
	}
	return res.StatusCode, out, anyBody
}

func TestARepositoryListsItsRemotesWithOriginFirstAndNoCredentialInTheURL(t *testing.T) {
	g, origin := startWatch(t, ":memory:", true)
	g.setup("remote add aaa-mirror https://bob:s3cret@git.example.invalid/team/repo.git")
	code, remotes, body := g.remotesOf(g.repo)
	if code != http.StatusOK || len(remotes) != 2 || remotes[0].Name != "origin" || remotes[0].URL != origin ||
		remotes[1].Name != "aaa-mirror" || remotes[1].URL != "https://git.example.invalid/team/repo.git" {
		t.Fatalf("remotes = %d %+v %v", code, remotes, body)
	}
	if strings.Contains(fmt.Sprint(body), "s3cret") {
		t.Errorf("the answer holds a password: %v", body)
	}
}

func TestARepositoryWithNoRemoteListsNoneAndAnUnknownRepositoryIsRefused(t *testing.T) {
	g, _ := startWatch(t, ":memory:", false)
	if code, remotes, body := g.remotesOf(g.repo); code != http.StatusOK || len(remotes) != 0 || body["remotes"] == nil {
		t.Errorf("no remote: %d %+v %v", code, remotes, body)
	}
	if code, _, body := g.remotesOf("no-such-repository"); code != http.StatusNotFound || body["code"] != "REPOSITORY_NOT_FOUND" {
		t.Errorf("unknown repository: %d %v", code, body)
	}
}
