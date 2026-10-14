package web

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// historyPages are the code fixture's pages with its history readable: its
// commits have diffs, so a commit's detail carries the diff's own seed.
func historyPages(t *testing.T) *codePages {
	t.Helper()
	return newCodePages(t, func(c *codePages, d *Deps) {
		d.History = gitHistory{dirs: map[string]string{c.at.RepositoryID: c.f.RepoDir}}
	})
}

func TestHistoryPageAnswersASwapWithOnlyTheCommitDetail(t *testing.T) {
	c := historyPages(t)
	base := "/projects/" + c.f.ProjectID + "/repositories/" + c.at.RepositoryID + "/history"
	for _, path := range []string{base, base + "/" + c.f.Rename} {
		full := get(t, c.h, path)
		if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), "<html") || !strings.Contains(full.Body.String(), `aria-label="Commits"`) {
			t.Fatalf("%s: without HX-Request the full page, got %d", path, full.Code)
		}
		frag := swap(t, c.h, path, "commit-detail")
		body := frag.Body.String()
		if frag.Code != http.StatusOK {
			t.Fatalf("%s: fragment status %d: %s", path, frag.Code, body)
		}
		for _, absent := range []string{"<html", "<body", `aria-label="Commits"`, "Remote branches"} {
			if strings.Contains(body, absent) {
				t.Errorf("%s: the fragment carries %q, which is outside the swap region", path, absent)
			}
		}
		region := openingTag(body, `id="commit-detail"`)
		if !strings.Contains(region, "data-swap-region") || !strings.Contains(region, `aria-label="Commit"`) {
			t.Errorf("%s: the swap region is %q, want the commit's detail", path, region)
		}
		// The diff's seed is region-local: it travels with the region.
		if !strings.Contains(body, `x-data="codeDiff" data-seed="code-diff-seed"`) || !strings.Contains(body, `id="code-diff-seed"`) {
			t.Errorf("%s: the fragment lacks the diff and its own seed", path)
		}
		for _, rec := range []*httptest.ResponseRecorder{full, frag} {
			if got := rec.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("%s: Vary = %q, want HX-Request", path, got)
			}
		}
	}
	if body := swap(t, c.h, base+"/"+c.f.Rename, "commit-detail").Body.String(); !strings.Contains(body, c.f.Rename) || !strings.Contains(body, "docs/new.md") {
		t.Errorf("the fragment is not the named commit's detail:\n%s", body)
	}
}

// hrefOf is the href attribute of a start tag, unescaped.
func hrefOf(tag string) string {
	_, after, ok := strings.Cut(tag, `href="`)
	if !ok {
		return ""
	}
	href, _, _ := strings.Cut(after, `"`)
	return strings.ReplaceAll(href, "&amp;", "&")
}

func TestHistoryPageRowsAndParentsAreLinksThatSwapTheDetail(t *testing.T) {
	w := historyWorld(t)
	h := newTestHandler(t, w)
	commits, err := w.history.RepositoryLog(context.Background(), "r1", ports.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var merge domain.Commit
	for _, c := range commits {
		if c.Subject == "Merge the docs" {
			merge = c
		}
	}
	body := get(t, h, "/projects/p1/repositories/r1/history/"+merge.Hash+"?limit=50").Body.String()
	rows := 0
	for rest := body; ; rows++ {
		i := strings.Index(rest, `<a class="[ commit ]"`)
		if i < 0 {
			break
		}
		tag := openingTag(rest[i:], `class="[ commit ]"`)
		assertSwapTrigger(t, tag, hrefOf(tag), "commit-detail")
		if !strings.Contains(hrefOf(tag), "?limit=50#c-") {
			t.Errorf("row %s does not keep the query and its place in the graph", tag)
		}
		rest = rest[i+len(tag):]
	}
	if rows != 5 {
		t.Errorf("%d commit rows, want 5", rows)
	}
	parents := 0
	for rest := body; ; parents++ {
		i := strings.Index(rest, `<a class="[ code ]"`)
		if i < 0 {
			break
		}
		tag := openingTag(rest[i:], `class="[ code ]"`)
		assertSwapTrigger(t, tag, hrefOf(tag), "commit-detail")
		rest = rest[i+len(tag):]
	}
	if parents != 2 {
		t.Errorf("%d parent links on a merge, want 2", parents)
	}
}

func TestHistoryPageSwapKeepsTheRefusals(t *testing.T) {
	h := newTestHandler(t, historyWorld(t))
	for _, path := range []string{
		"/projects/p1/repositories/r1/history/deadbeef",   // no such commit
		"/projects/p1/repositories/r1/history/--output=x", // not a hash at all
		"/projects/p2/repositories/r1/history",            // another project's repository
		"/projects/p1/repositories/unknown/history",
	} {
		if rec := swap(t, h, path, "commit-detail"); rec.Code != http.StatusNotFound {
			t.Errorf("%s with HX-Request: status %d, want 404", path, rec.Code)
		}
	}
}

// agentCommit is the hash of the session's own commit in historyWorld.
func agentCommit(t *testing.T, w world) string {
	t.Helper()
	commits, err := w.history.RepositoryLog(context.Background(), "r1", ports.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		if c.Subject == "Add the SSE feed" {
			return c.Hash
		}
	}
	t.Fatal("no agent commit")
	return ""
}

func TestSessionHistoryAnswersASwapWithOnlyTheDetail(t *testing.T) {
	w := historyWorld(t)
	w.prs = fakePullRequests{views: map[string]*ports.PullRequestView{"ws-agent": {Draft: agentDraft()}}}
	h := newTestHandler(t, w)
	agent := agentCommit(t, w)
	for path, detail := range map[string]string{
		sessionHistory:                            `aria-label="Uncommitted changes"`, // open by default
		sessionHistory + "/working":               `aria-label="Uncommitted changes"`,
		sessionHistory + "/" + agent:              `aria-label="Commit"`,
		sessionHistory + "/" + agent + "?limit=3": `aria-label="Commit"`,
	} {
		full := get(t, h, path)
		if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), `x-data="pullRequestPanel"`) {
			t.Fatalf("%s: without HX-Request the full page with its pull request panel, got %d", path, full.Code)
		}
		frag := swap(t, h, path, "commit-detail")
		body := frag.Body.String()
		if frag.Code != http.StatusOK {
			t.Fatalf("%s: fragment status %d: %s", path, frag.Code, body)
		}
		for _, absent := range []string{"<html", `aria-label="Commits"`, "pullRequestPanel", "pull-request-seed", "Git history"} {
			if strings.Contains(body, absent) {
				t.Errorf("%s: the fragment carries %q, which is outside the swap region", path, absent)
			}
		}
		if region := openingTag(body, `id="commit-detail"`); !strings.Contains(region, "data-swap-region") || !strings.Contains(region, detail) {
			t.Errorf("%s: the swap region is %q, want %s", path, region, detail)
		}
		for _, rec := range []*httptest.ResponseRecorder{full, frag} {
			if got := rec.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("%s: Vary = %q, want HX-Request", path, got)
			}
		}
	}
}

func TestSessionHistoryRowsAreLinksThatSwapTheDetail(t *testing.T) {
	w := historyWorld(t)
	body := get(t, newTestHandler(t, w), sessionHistory+"/"+agentCommit(t, w)).Body.String()
	working := openingTag(body, "data-working")
	if hrefOf(working) != sessionHistory+"/working" {
		t.Fatalf("the uncommitted changes row is %q", working)
	}
	assertSwapTrigger(t, working, hrefOf(working), "commit-detail")
	rows := 0
	for rest := body; ; rows++ {
		i := strings.Index(rest, `<a class="[ commit ]"`)
		if i < 0 {
			break
		}
		tag := openingTag(rest[i:], `class="[ commit ]"`)
		assertSwapTrigger(t, tag, hrefOf(tag), "commit-detail")
		rest = rest[i+len(tag):]
	}
	if rows < 2 {
		t.Errorf("%d rows, want the changes and the commits", rows)
	}
}

func TestSessionHistorySwapKeepsTheRefusals(t *testing.T) {
	h := newTestHandler(t, historyWorld(t))
	for _, path := range []string{
		sessionHistory + "/deadbeef",                         // no such commit
		"/projects/p1/tasks/t-docs/sessions/s-agent/history", // another task's
		"/projects/p1/tasks/t-feed/sessions/s1/history",      // no worktree
		"/projects/p1/tasks/t-feed/sessions/nope/history",
	} {
		if rec := swap(t, h, path, "commit-detail"); rec.Code != http.StatusNotFound {
			t.Errorf("%s with HX-Request: status %d, want 404", path, rec.Code)
		}
	}
}

// showMore is the request htmx sends for a history's "Show more": the next
// page's URL, into the list's tail, after the last commit shown.
func showMore(t *testing.T, h http.Handler, path, after string) *httptest.ResponseRecorder {
	t.Helper()
	if after == "" {
		return swap(t, h, path, "history-more")
	}
	return swap(t, h, path, "history-more", "X-History-After", after)
}

// commitRows are the hashes of the commit rows in body, in order.
func commitRows(body string) []string {
	var out []string
	for rest := body; ; {
		i := strings.Index(rest, `<a class="[ commit ]"`)
		if i < 0 {
			return out
		}
		tag := openingTag(rest[i:], `class="[ commit ]"`)
		if !strings.Contains(tag, "data-working") {
			path, _, _ := strings.Cut(hrefOf(tag), "?")
			out = append(out, path[strings.LastIndex(path, "/")+1:])
		}
		rest = rest[i+len(tag):]
	}
}

func TestHistoryShowMoreIsALinkThatAppendsAfterTheLastCommitShown(t *testing.T) {
	w := historyWorld(t)
	h := newTestHandler(t, w)
	for _, base := range []string{"/projects/p1/repositories/r1/history", sessionHistory} {
		body := get(t, h, base+"?limit=1").Body.String()
		shown := commitRows(body)
		if len(shown) != 1 {
			t.Fatalf("%s: %d rows shown, want 1", base, len(shown))
		}
		more := openingTag(body, `href="`+base+`?limit=201"`)
		assertSwapTrigger(t, more, base+"?limit=201", "history-more")
		if !strings.Contains(more, `hx-headers="{&#34;X-History-After&#34;:&#34;`+shown[0]+`&#34;}"`) {
			t.Errorf("%s: Show more does not say what it follows: %s", base, more)
		}
		if region := openingTag(body, `id="history-more"`); region == "" || strings.Contains(region, "data-swap-region") {
			t.Errorf("%s: Show more's target is %q, want a plain tail of the list", base, region)
		}
	}
}

func TestHistoryShowMoreAnswersOnlyTheNextCommits(t *testing.T) {
	w := historyWorld(t)
	h := newTestHandler(t, w)
	for _, base := range []string{"/projects/p1/repositories/r1/history", sessionHistory} {
		all := commitRows(get(t, h, base+"?limit=201").Body.String())
		if len(all) < 3 {
			t.Fatalf("%s: %d commits, want a few", base, len(all))
		}
		rec := showMore(t, h, base+"?limit=201", all[0])
		body := rec.Body.String()
		if rec.Code != http.StatusOK || rec.Header().Get("Vary") != "HX-Request" {
			t.Fatalf("%s: status %d, Vary %q", base, rec.Code, rec.Header().Get("Vary"))
		}
		if got := strings.Join(commitRows(body), ","); got != strings.Join(all[1:], ",") {
			t.Errorf("%s: appended %s, want the commits after the first: %s", base, got, strings.Join(all[1:], ","))
		}
		// Never a commit's detail, nor the page around the list.
		for _, absent := range []string{`id="commit-detail"`, "data-working", "<html", `aria-label="Commits"`, "Show more"} {
			if strings.Contains(body, absent) {
				t.Errorf("%s: the next commits carry %q", base, absent)
			}
		}
		// And a commit swap is never the next commits.
		detail := swap(t, h, base+"/"+all[1]+"?limit=201", "commit-detail").Body.String()
		if rows := commitRows(detail); len(rows) != 0 || strings.Contains(detail, `id="history-more"`) {
			t.Errorf("%s: a commit swap carries rows %v or Show more", base, rows)
		}
	}
}

func TestHistoryShowMoreAfterACommitNoLongerListedIsRefused(t *testing.T) {
	h := newTestHandler(t, historyWorld(t))
	for _, after := range []string{"0000000000000000000000000000000000000000", ""} {
		if rec := showMore(t, h, "/projects/p1/repositories/r1/history?limit=201", after); rec.Code != http.StatusConflict {
			t.Errorf("after %q: status %d, want 409 (the page loads instead)", after, rec.Code)
		}
	}
}

func TestHistoryShowMoreKeepsTheOpenCommit(t *testing.T) {
	w := historyWorld(t)
	h := newTestHandler(t, w)
	agent := agentCommit(t, w)
	for page, want := range map[string]string{
		"/projects/p1/repositories/r1/history/" + agent + "?limit=1": "/projects/p1/repositories/r1/history/" + agent + "?limit=201",
		sessionHistory + "/" + agent + "?limit=1":                    sessionHistory + "/" + agent + "?limit=201",
		sessionHistory + "/working?limit=1":                          sessionHistory + "/working?limit=201",
		sessionHistory + "?limit=1":                                  sessionHistory + "?limit=201", // none named: as before
	} {
		body := get(t, h, page).Body.String()
		if more := hrefOf(openingTag(body, `hx-target="#history-more"`)); more != want {
			t.Errorf("%s: Show more is %q, want %q", page, more, want)
		}
	}
}
