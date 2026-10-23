package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func gitTrim(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mergeRepo: main with README.md, and a branch agent/x with one commit that
// adds agent.txt. The repository has its own committer identity.
func mergeRepo(t *testing.T) string {
	t.Helper()
	repo := initRepo(t)
	gitIn(t, repo, "config", "user.name", "test")
	gitIn(t, repo, "config", "user.email", "test@test")
	gitIn(t, repo, "branch", "agent/x")
	gitIn(t, repo, "checkout", "-q", "agent/x")
	put(t, repo, "agent.txt", "a\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "agent work")
	gitIn(t, repo, "checkout", "-q", "main")
	return repo
}

func TestMerger_CheckoutReadsBranchHeadAndWhatIsUncommitted(t *testing.T) {
	repo := mergeRepo(t)
	m := NewMerger()
	st, err := m.Checkout(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Head != gitTrim(t, repo, "rev-parse", "HEAD") || !st.Clean() {
		t.Fatalf("clean checkout = %+v", st)
	}
	put(t, repo, "README.md", "changed\n")
	put(t, repo, "new file.txt", "x\n")
	put(t, repo, ".gitignore", "ignored.log\n")
	put(t, repo, "ignored.log", "x\n")
	gitIn(t, repo, "add", ".gitignore")
	st, err = m.Checkout(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.Tracked, ","); got != ".gitignore,README.md" {
		t.Errorf("tracked = %q", got)
	}
	if got := strings.Join(st.Untracked, ","); got != "new file.txt" {
		t.Errorf("untracked = %q: ignored files are not uncommitted", got)
	}
	gitIn(t, repo, "checkout", "-q", "--detach")
	if st, _ = m.Checkout(context.Background(), repo); st.Branch != "" {
		t.Errorf("detached HEAD has branch %q", st.Branch)
	}
	_, err = m.Checkout(context.Background(), t.TempDir())
	if errs.Code(err) != domain.MergeNoCheckout {
		t.Errorf("a directory that is no checkout: %v", err)
	}
}

func TestMerger_MergesWithAMergeCommit(t *testing.T) {
	repo := mergeRepo(t)
	m := NewMerger()
	before := gitTrim(t, repo, "rev-parse", "HEAD")
	head, err := m.Merge(context.Background(), repo, "agent/x")
	if err != nil {
		t.Fatal(err)
	}
	if head != gitTrim(t, repo, "rev-parse", "HEAD") || head == before {
		t.Fatalf("head = %s, before %s", head, before)
	}
	if parents := strings.Fields(gitTrim(t, repo, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 3 {
		t.Fatalf("not a merge commit: %v", parents)
	}
	if _, err := os.Stat(filepath.Join(repo, "agent.txt")); err != nil {
		t.Fatal("the branch's file is not in the checkout")
	}
	if gitTrim(t, repo, "status", "--porcelain") != "" {
		t.Error("the checkout is not clean after the merge")
	}
}

func TestMerger_RefusesWhatIsAlreadyMergedAndWhatIsMissing(t *testing.T) {
	repo := mergeRepo(t)
	m := NewMerger()
	if _, err := m.Merge(context.Background(), repo, "agent/gone"); errs.Code(err) != domain.MergeBranchNotFound {
		t.Errorf("missing branch: %v", err)
	}
	if _, err := m.Merge(context.Background(), repo, "agent/x"); err != nil {
		t.Fatal(err)
	}
	head := gitTrim(t, repo, "rev-parse", "HEAD")
	if _, err := m.Merge(context.Background(), repo, "agent/x"); errs.Code(err) != domain.MergeAlreadyMerged {
		t.Errorf("merged twice: %v", err)
	}
	if gitTrim(t, repo, "rev-parse", "HEAD") != head {
		t.Error("a refused merge moved HEAD")
	}
	if !m.HasBranch(context.Background(), repo, "agent/x") || m.HasBranch(context.Background(), repo, "agent/gone") {
		t.Error("HasBranch")
	}
}

func TestMerger_AConflictIsAbortedAndLeavesTheCheckoutAsItWas(t *testing.T) {
	repo := mergeRepo(t)
	gitIn(t, repo, "checkout", "-q", "agent/x")
	put(t, repo, "README.md", "from the agent\n")
	gitIn(t, repo, "commit", "-q", "-am", "agent edits README")
	gitIn(t, repo, "checkout", "-q", "main")
	put(t, repo, "README.md", "from main\n")
	gitIn(t, repo, "commit", "-q", "-am", "main edits README")
	head := gitTrim(t, repo, "rev-parse", "HEAD")
	index := gitTrim(t, repo, "write-tree")

	_, err := NewMerger().Merge(context.Background(), repo, "agent/x")
	if errs.Code(err) != domain.MergeConflict || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("conflict = %v", err)
	}
	if gitTrim(t, repo, "rev-parse", "HEAD") != head || gitTrim(t, repo, "write-tree") != index || gitTrim(t, repo, "status", "--porcelain") != "" {
		t.Error("the checkout is not as it was")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(b) != "from main\n" {
		t.Errorf("README.md = %q", b)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a merge is still in progress")
	}
}

func TestMerger_AGitThatAnswersNotInTimeFailsTheMerge(t *testing.T) {
	repo := mergeRepo(t)
	hook := filepath.Join(repo, ".git", "hooks", "pre-merge-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitTrim(t, repo, "rev-parse", "HEAD")
	m := NewMerger(WithWorktreeDeadline(500 * 1e6))
	_, err := m.Merge(context.Background(), repo, "agent/x")
	if errs.Code(err) != domain.MergeFailed {
		t.Fatalf("a hung hook: %v", err)
	}
	if gitTrim(t, repo, "rev-parse", "HEAD") != head || gitTrim(t, repo, "status", "--porcelain") != "" {
		t.Error("the checkout is not as it was")
	}
}
