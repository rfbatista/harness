package gitcli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// historyRepo: main → feature branched and merged back, a tag, and an
// unmerged branch "agent/x" (as a session's).
//
//   - merge feature        (main, v1)
//     |\
//     | * feature work         (feature)
//   - | main work
//     |/
//     | * agent work           (agent/x)
//     |/
//   - init
func historyRepo(t *testing.T) string {
	t.Helper()
	repo := initRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "branch", "agent/x")
	gitIn(t, repo, "checkout", "-q", "agent/x")
	write("agent.txt", "a\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "agent work")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "checkout", "-q", "-b", "feature")
	write("feature.txt", "f1\nf2\n")
	write("logo.bin", "\x00\x01\x02")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "feature work")
	gitIn(t, repo, "checkout", "-q", "main")
	write("README.md", "hi\nthere\n")
	gitIn(t, repo, "commit", "-q", "-am", "main work")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge feature\n\nBrings the feature in.", "feature")
	gitIn(t, repo, "tag", "v1")
	return repo
}

func TestHistory_LogsTheGraphChildrenFirstWithRefs(t *testing.T) {
	repo := historyRepo(t)
	commits, err := NewHistory().Log(context.Background(), repo, ports.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	subjects := make([]string, len(commits))
	for i, c := range commits {
		subjects[i] = c.Subject
	}
	if len(commits) != 5 || subjects[4] != "init" {
		t.Fatalf("subjects = %v", subjects)
	}
	merge := bySubject(commits, "merge feature")
	if len(merge.Parents) != 2 || merge.AuthorName != "test" || merge.AuthoredAt.IsZero() {
		t.Fatalf("merge = %+v", merge)
	}
	if got := refNames(merge.Refs); got != "branch:main(head) tag:v1" {
		t.Errorf("merge refs = %q", got)
	}
	// Every parent of a listed commit comes after it.
	seen := map[string]bool{}
	for _, c := range commits {
		for _, p := range c.Parents {
			if seen[p] {
				t.Errorf("parent %s listed before its child %s", p[:7], c.Hash[:7])
			}
		}
		seen[c.Hash] = true
	}
	agent := bySubject(commits, "agent work")
	if refNames(agent.Refs) != "branch:agent/x" {
		t.Errorf("an unmerged branch is in the graph with its ref: %+v", agent)
	}

	limited, _ := NewHistory().Log(context.Background(), repo, ports.HistoryQuery{Limit: 2})
	if len(limited) != 2 {
		t.Errorf("limit 2 gave %d", len(limited))
	}
}

func TestHistory_ShowsAMergeAgainstItsFirstParent(t *testing.T) {
	repo := historyRepo(t)
	commits, _ := NewHistory().Log(context.Background(), repo, ports.HistoryQuery{})
	d, err := NewHistory().Show(context.Background(), repo, bySubject(commits, "merge feature").Hash[:10])
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "merge feature" || d.Body != "Brings the feature in." || len(d.Parents) != 2 {
		t.Fatalf("detail = %+v", d)
	}
	var files []string
	for _, f := range d.Files {
		files = append(files, f.Path+" +"+itoa(f.Added)+" -"+itoa(f.Deleted))
	}
	if got := strings.Join(files, ", "); got != "feature.txt +2 -0, logo.bin +-1 --1" {
		t.Errorf("files = %s", got)
	}
}

func TestHistory_RefusesWhatIsNotACommitHash(t *testing.T) {
	repo := historyRepo(t)
	for _, bad := range []string{"--output=/tmp/x", "main", "HEAD", "deadbeef", ""} {
		if _, err := NewHistory().Show(context.Background(), repo, bad); errs.Code(err) != "COMMIT_NOT_FOUND" {
			t.Errorf("Show(%q) = %v, want COMMIT_NOT_FOUND", bad, err)
		}
	}
}

func TestHistory_AnEmptyRepositoryHasNoHistory(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	commits, err := NewHistory().Log(context.Background(), dir, ports.HistoryQuery{})
	if err != nil || len(commits) != 0 {
		t.Fatalf("got %v, %v", commits, err)
	}
}

func refNames(refs []domain.GitRef) string {
	var out []string
	for _, r := range refs {
		s := string(r.Kind) + ":" + r.Name
		if r.Head {
			s += "(head)"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func itoa(n int) string { return strconv.Itoa(n) }

func bySubject(commits []domain.Commit, subject string) domain.Commit {
	for _, c := range commits {
		if c.Subject == subject {
			return c
		}
	}
	return domain.Commit{}
}

func TestHistory_LogsOnlyTheRefsAsked(t *testing.T) {
	repo := historyRepo(t)
	commits, err := NewHistory().Log(context.Background(), repo, ports.HistoryQuery{Refs: []string{"agent/x", "--all", "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range commits {
		subjects = append(subjects, c.Subject)
	}
	if strings.Join(subjects, ",") != "agent work,init" {
		t.Fatalf("subjects = %v (an option-like or unknown ref must not widen the log)", subjects)
	}
	if none, _ := NewHistory().Log(context.Background(), repo, ports.HistoryQuery{Refs: []string{"nope"}}); len(none) != 0 {
		t.Fatalf("an unknown ref logs nothing: %v", none)
	}
}

// A session's worktree: a branch off main with a commit, then uncommitted
// work — staged, unstaged, a rename, an untracked file.
func TestHistory_StatusAndDivergenceOfAWorktree(t *testing.T) {
	repo := historyRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "agent/y", wt, "main")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(wt, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("y.txt", "1\n")
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-q", "-m", "y work")
	write("README.md", "hi\nthere\nagain\n") // unstaged: +1
	write("staged.txt", "s1\ns2\n")          // staged new: +2
	gitIn(t, wt, "add", "staged.txt")
	gitIn(t, wt, "mv", "feature.txt", "renamed.txt") // a rename
	write("scratch.txt", "untracked\n")              // untracked

	st, err := NewHistory().Status(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "agent/y" || len(st.Head) != 40 {
		t.Fatalf("status = %+v", st)
	}
	got := map[string]string{}
	for _, c := range st.Changes {
		got[c.Path] = c.Status + " " + strconv.Itoa(c.Added) + "/" + strconv.Itoa(c.Deleted)
	}
	want := map[string]string{
		"README.md":   " M 1/0",
		"staged.txt":  "A  2/0",
		"renamed.txt": "R  0/0",
		"scratch.txt": "?? -1/-1",
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s = %q, want %q (all: %v)", path, got[path], w, got)
		}
	}

	ahead, behind, err := NewHistory().Divergence(context.Background(), wt, "main", "agent/y")
	if err != nil || ahead != 1 || behind != 0 {
		t.Fatalf("divergence = %d ahead, %d behind, %v", ahead, behind, err)
	}

	if _, err := NewHistory().Status(context.Background(), filepath.Join(t.TempDir(), "gone")); errs.Code(err) != "WORKSPACE_MISSING" {
		t.Fatalf("a removed worktree: %v", err)
	}
}
