package web

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// gitHistory is the history port over real checkouts: repositories by id,
// and workspaces (a worktree on a branch, cut from base) by id.
type gitHistory struct {
	dirs       map[string]string
	workspaces map[string]testWorkspace
}

type testWorkspace struct{ repo, dir, branch, base string }

// WorkspaceHistory mirrors workspaces.Service.WorkspaceHistory over gitcli.
func (g gitHistory) WorkspaceHistory(ctx context.Context, id string, limit int) (*domain.WorkspaceHistory, error) {
	ws, ok := g.workspaces[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "WORKSPACE_NOT_FOUND", Message: "workspace not found"}
	}
	h := gitcli.NewHistory()
	out := &domain.WorkspaceHistory{WorkspaceID: id, RepositoryID: ws.repo, Branch: ws.branch, Base: ws.base}
	root := g.dirs[ws.repo]
	if st, err := h.Status(ctx, ws.dir); err == nil {
		out.Head, out.Changes = st.Head, st.Changes
	} else {
		out.Gone = true
	}
	out.Ahead, out.Behind, _ = h.Divergence(ctx, root, ws.base, ws.branch)
	var err error
	out.Commits, err = h.Log(ctx, root, ports.HistoryQuery{Limit: limit, Refs: []string{ws.branch, ws.base}})
	return out, err
}

func (g gitHistory) RepositoryLog(ctx context.Context, id string, q ports.HistoryQuery) ([]domain.Commit, error) {
	dir, ok := g.dirs[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return gitcli.NewHistory().Log(ctx, dir, q)
}

func (g gitHistory) RepositoryCommit(ctx context.Context, id, hash string) (*domain.CommitDetail, error) {
	dir, ok := g.dirs[id]
	if !ok {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	return gitcli.NewHistory().Show(ctx, dir, hash)
}

// historyRepo: main with a merged feature branch, a tag, and an unmerged
// session branch "agent/add-sse-feed-1a2b".
func historyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Lead dev", "-c", "user.email=lead@test", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("README.md", "hi\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("checkout", "-q", "-b", "agent/add-sse-feed-1a2b")
	write("feed.go", "package feed\n")
	git("add", ".")
	git("commit", "-q", "-m", "Add the SSE feed")
	git("checkout", "-q", "main")
	git("checkout", "-q", "-b", "feature")
	write("docs.md", "# Docs\n")
	git("add", ".")
	git("commit", "-q", "-m", "Write the docs")
	git("checkout", "-q", "main")
	write("README.md", "hi\nthere\n")
	git("commit", "-q", "-am", "Tidy the README")
	git("merge", "-q", "--no-ff", "-m", "Merge the docs\n\nReviewed by the lead.", "feature")
	git("tag", "v1")
	return dir
}

func historyWorld(t *testing.T) world {
	w := board()
	w.repos = []*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness", RootDir: "/x"}}
	w.sessions = append(w.sessions, &domain.Session{ID: "s-agent", ProjectID: "p1", TicketID: "t-feed", RepositoryID: "r1", Task: "build the feed",
		Branch: "agent/add-sse-feed-1a2b", Status: domain.SessionRunning, UpdatedAt: now})
	repo := historyRepo(t)
	// The session works in a worktree on its branch, with work not yet committed.
	wt := filepath.Join(t.TempDir(), "wt")
	gitCmd(t, repo, "worktree", "add", "-q", wt, "agent/add-sse-feed-1a2b")
	if err := os.WriteFile(filepath.Join(wt, "feed.go"), []byte("package feed\n\nfunc Follow() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "notes.md"), []byte("todo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.sessions[len(w.sessions)-1].WorkspaceID = "ws-agent"
	w.history = gitHistory{
		dirs:       map[string]string{"r1": repo},
		workspaces: map[string]testWorkspace{"ws-agent": {repo: "r1", dir: wt, branch: "agent/add-sse-feed-1a2b", base: "main"}},
	}
	return w
}

func TestHistoryPageDrawsTheGraphAndOpensTheNewestCommit(t *testing.T) {
	rec := get(t, newTestHandler(t, historyWorld(t)), "/projects/p1/repositories/r1/history")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"5 commits",
		`<svg class="[ graph ]"`,
		"Merge the docs",
		`data-merge`,
		`data-kind="tag"`,
		`data-head`,                       // main, checked out
		`data-kind="branch" data-session`, // the session's branch, marked
		`aria-label="Commit"`,             // the newest commit is open
		`aria-current="page"`,
		`aria-pressed="false"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestHistoryPageOpensANamedCommitAndLinksSessionBranches(t *testing.T) {
	w := historyWorld(t)
	commits, err := w.history.RepositoryLog(context.Background(), "r1", ports.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var agent domain.Commit
	for _, c := range commits {
		if c.Subject == "Add the SSE feed" {
			agent = c
		}
	}
	rec := get(t, newTestHandler(t, w), "/projects/p1/repositories/r1/history/"+agent.Hash)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "feed.go") {
		t.Fatalf("status %d; the agent's commit is not open:\n%s", rec.Code, body)
	}
	if i := strings.Index(body, `data-session href=`); i < 0 || !strings.HasPrefix(body[i:], `data-session href="/projects/p1/tasks/t-feed"`) {
		// (the attribute order is the template's)
		t.Errorf("the open commit's session branch does not link its task: %s", around(body, "data-session"))
	}
}

func TestHistoryPageShowsMoreAndTogglesRemotes(t *testing.T) {
	body := get(t, newTestHandler(t, historyWorld(t)), "/projects/p1/repositories/r1/history?limit=2").Body.String()
	if !strings.Contains(body, "2 commits") || !strings.Contains(body, `href="/projects/p1/repositories/r1/history?limit=202"`) {
		t.Errorf("a full page offers more:\n%s", body)
	}
	if !strings.Contains(body, `href="/projects/p1/repositories/r1/history?limit=2&amp;remotes=1"`) {
		t.Error("the remotes toggle keeps the limit")
	}
	if !strings.Contains(body, `/history/`) || !strings.Contains(body, `?limit=2#c-`) {
		t.Error("commit links keep the query and the place in the graph")
	}
}

func TestHistoryPageRefusals(t *testing.T) {
	h := newTestHandler(t, historyWorld(t))
	for path, want := range map[string]int{
		"/projects/p1/repositories/r1/history/deadbeef":   http.StatusNotFound, // no such commit
		"/projects/p1/repositories/r1/history/--output=x": http.StatusNotFound, // not a hash at all
		"/projects/p2/repositories/r1/history":            http.StatusNotFound, // another project's repository
		"/projects/p1/repositories/unknown/history":       http.StatusNotFound,
	} {
		if rec := get(t, h, path); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}

func TestRepositoriesPageLinksHistory(t *testing.T) {
	body := get(t, newTestHandler(t, historyWorld(t)), "/projects/p1/repositories").Body.String()
	if !strings.Contains(body, `href="/projects/p1/repositories/r1/history"`) || !strings.Contains(body, `x-bind:href="r.historyHref"`) {
		t.Error("the repositories page does not link a repository's history")
	}
}

// A merge opens with its whole message and its files against the first parent.
func TestHistoryPageOpensAMerge(t *testing.T) {
	w := historyWorld(t)
	commits, _ := w.history.RepositoryLog(context.Background(), "r1", ports.HistoryQuery{})
	for _, c := range commits {
		if c.Subject != "Merge the docs" {
			continue
		}
		body := get(t, newTestHandler(t, w), "/projects/p1/repositories/r1/history/"+c.Hash).Body.String()
		for _, want := range []string{"Reviewed by the lead.", "docs.md", "Parents", "1 file changed · +1 −0"} {
			if !strings.Contains(body, want) {
				t.Errorf("merge page lacks %q", want)
			}
		}
		return
	}
	t.Fatal("no merge in the history")
}

func around(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "(absent)"
	}
	return s[max(0, i-200):min(len(s), i+200)]
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestSessionHistoryShowsItsBranchAgainstItsBaseAndWhatIsUncommitted(t *testing.T) {
	rec := get(t, newTestHandler(t, historyWorld(t)), "/projects/p1/tasks/t-feed/sessions/s-agent/history")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Git history",
		"agent/add-sse-feed-1a2b · 1 ahead, 3 behind main", // main gained the docs, the README tidy and their merge
		"Uncommitted changes",
		"2 uncommitted changes",
		`aria-label="Uncommitted changes"`, // open by default
		"notes.md", "untracked",
		"feed.go", "modified",
		`data-mine`,                        // the session's own branch
		`href="/projects/p1/tasks/t-feed"`, // back to its task
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "Write the docs") && !strings.Contains(body, "Merge the docs") {
		t.Error("the base's history is incomplete")
	}
	if strings.Contains(body, "Remote branches") {
		t.Error("the session page has no remotes toggle")
	}
}

func TestSessionHistoryOpensACommitAndRefusesOtherTasksSessions(t *testing.T) {
	w := historyWorld(t)
	h := newTestHandler(t, w)
	commits, _ := w.history.RepositoryLog(context.Background(), "r1", ports.HistoryQuery{})
	var agent string
	for _, c := range commits {
		if c.Subject == "Add the SSE feed" {
			agent = c.Hash
		}
	}
	rec := get(t, h, "/projects/p1/tasks/t-feed/sessions/s-agent/history/"+agent)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `aria-label="Commit"`) {
		t.Fatalf("status %d; the commit is not open", rec.Code)
	}
	for path, want := range map[string]int{
		"/projects/p1/tasks/t-docs/sessions/s-agent/history": http.StatusNotFound, // another task's
		"/projects/p1/tasks/t-feed/sessions/s1/history":      http.StatusNotFound, // no worktree
		"/projects/p1/tasks/t-feed/sessions/nope/history":    http.StatusNotFound,
	} {
		if rec := get(t, h, path); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}
