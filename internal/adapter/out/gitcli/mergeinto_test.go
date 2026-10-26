package gitcli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

// checkoutSnapshot is everything about a main checkout a merge into another
// branch must leave alone: its branch, HEAD, status, the raw index file and
// every file on disk, ignored and untracked ones included.
func checkoutSnapshot(t *testing.T, repo string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(gitTrim(t, repo, "rev-parse", "--abbrev-ref", "HEAD") + "\n")
	b.WriteString(gitTrim(t, repo, "rev-parse", "HEAD") + "\n")
	b.WriteString(gitTrim(t, repo, "status", "--porcelain=v2", "--branch", "--untracked-files=all") + "\n")
	idx, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	b.WriteString(string(idx) + "\n")
	err = filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if !d.IsDir() {
			body, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(repo, p)
			b.WriteString(rel + "\x00" + string(body) + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// dirtyCheckout leaves the person's own work in the main checkout: a staged
// new file, an edit to a tracked file, an untracked file and an ignored one.
func dirtyCheckout(t *testing.T, repo string) {
	t.Helper()
	put(t, repo, ".gitignore", "ignored.log\n")
	gitIn(t, repo, "add", ".gitignore")
	gitIn(t, repo, "commit", "-q", "-m", "ignore")
	put(t, repo, "staged.txt", "s\n")
	gitIn(t, repo, "add", "staged.txt")
	put(t, repo, "README.md", "my edit\n")
	put(t, repo, "untracked.txt", "u\n")
	put(t, repo, "ignored.log", "i\n")
}

func mergeInto(m *Merger, repo, target, branch string, method domain.MergeMethod, msg string, checkedOut bool) (string, error) {
	return m.MergeInto(context.Background(), repo, target, branch, method, msg, checkedOut)
}

func TestMergeInto_ATargetThatIsNotCheckedOutLeavesTheMainCheckoutIdentical(t *testing.T) {
	for _, method := range []domain.MergeMethod{domain.MergeMethodMerge, domain.MergeMethodSquash} {
		t.Run(string(method), func(t *testing.T) {
			repo := mergeRepo(t)
			gitIn(t, repo, "branch", "release", "main")
			dirtyCheckout(t, repo)
			before := checkoutSnapshot(t, repo)
			releaseTip := gitTrim(t, repo, "rev-parse", "release")
			agentTip := gitTrim(t, repo, "rev-parse", "agent/x")
			mainTip := gitTrim(t, repo, "rev-parse", "main")

			head, err := mergeInto(NewMerger(), repo, "release", "agent/x", method, "ship it\n\nbody", false)
			if err != nil {
				t.Fatal(err)
			}
			if got := gitTrim(t, repo, "rev-parse", "release"); got != head || head == releaseTip {
				t.Fatalf("release = %s, head %s, was %s", got, head, releaseTip)
			}
			if after := checkoutSnapshot(t, repo); after != before {
				t.Fatalf("the main checkout changed:\n--- before\n%s\n--- after\n%s", before, after)
			}
			if gitTrim(t, repo, "rev-parse", "agent/x") != agentTip || gitTrim(t, repo, "rev-parse", "main") != mainTip {
				t.Error("another branch moved")
			}
			if gitTrim(t, repo, "show", "release:agent.txt") != "a" {
				t.Error("the branch's file is not in release")
			}
			parents := strings.Fields(gitTrim(t, repo, "rev-list", "--parents", "-n1", head))[1:]
			subject := gitTrim(t, repo, "log", "-1", "--format=%s", head)
			if method == domain.MergeMethodMerge {
				if len(parents) != 2 || parents[0] != releaseTip || parents[1] != agentTip || subject != "Merge branch 'agent/x' into release" {
					t.Errorf("merge commit: parents %v, subject %q", parents, subject)
				}
			} else {
				if len(parents) != 1 || parents[0] != releaseTip || gitTrim(t, repo, "log", "-1", "--format=%B", head) != "ship it\n\nbody" {
					t.Errorf("squash commit: parents %v, message %q", parents, gitTrim(t, repo, "log", "-1", "--format=%B", head))
				}
				if n := gitTrim(t, repo, "rev-list", "--count", "release~1.."+head); n != "1" {
					t.Errorf("squash added %s commits", n)
				}
			}
		})
	}
}

func TestMergeInto_ASquashAndAMergeCommitHoldTheSameTree(t *testing.T) {
	repo := mergeRepo(t)
	gitIn(t, repo, "branch", "a", "main")
	gitIn(t, repo, "branch", "b", "main")
	m := NewMerger()
	ha, err := mergeInto(m, repo, "a", "agent/x", domain.MergeMethodMerge, "", false)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := mergeInto(m, repo, "b", "agent/x", domain.MergeMethodSquash, "msg", false)
	if err != nil {
		t.Fatal(err)
	}
	if gitTrim(t, repo, "rev-parse", ha+"^{tree}") != gitTrim(t, repo, "rev-parse", hb+"^{tree}") {
		t.Fatal("trees differ")
	}
}

func TestMergeInto_AConflictChangesNothingAndNamesItsFiles(t *testing.T) {
	for _, checkedOut := range []bool{false, true} {
		repo := mergeRepo(t)
		gitIn(t, repo, "checkout", "-q", "agent/x")
		put(t, repo, "README.md", "from the agent\n")
		gitIn(t, repo, "commit", "-q", "-am", "agent edits README")
		gitIn(t, repo, "checkout", "-q", "-b", "release", "main")
		put(t, repo, "README.md", "from release\n")
		gitIn(t, repo, "commit", "-q", "-am", "release edits README")
		if !checkedOut {
			gitIn(t, repo, "checkout", "-q", "main")
		}
		put(t, repo, "mine.txt", "wip\n")
		before := checkoutSnapshot(t, repo)
		refs := gitTrim(t, repo, "for-each-ref")

		for _, method := range []domain.MergeMethod{domain.MergeMethodMerge, domain.MergeMethodSquash} {
			_, err := mergeInto(NewMerger(), repo, "release", "agent/x", method, "m", checkedOut)
			var de *domain.DetailedError
			if errs.Code(err) != domain.MergeConflict || !errors.As(err, &de) || !slices.Equal(de.Details.(map[string]any)["files"].([]string), []string{"README.md"}) {
				t.Fatalf("checkedOut=%v %s: %v", checkedOut, method, err)
			}
			if checkoutSnapshot(t, repo) != before || gitTrim(t, repo, "for-each-ref") != refs {
				t.Fatalf("checkedOut=%v %s: something changed", checkedOut, method)
			}
		}
		if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); !errors.Is(err, os.ErrNotExist) {
			t.Error("a merge is in progress")
		}
	}
}

func TestMergeInto_RefusesWhatIsAlreadyMergedAndWhatIsMissing(t *testing.T) {
	repo := mergeRepo(t)
	gitIn(t, repo, "branch", "release", "main")
	m := NewMerger()
	if _, err := mergeInto(m, repo, "release", "agent/gone", "merge", "", false); errs.Code(err) != domain.MergeBranchNotFound {
		t.Errorf("missing branch: %v", err)
	}
	if _, err := mergeInto(m, repo, "nope", "agent/x", "merge", "", false); errs.Code(err) != domain.MergeTargetNotFound {
		t.Errorf("missing target: %v", err)
	}
	if _, err := mergeInto(m, repo, "release", "agent/x", "merge", "", false); err != nil {
		t.Fatal(err)
	}
	tip := gitTrim(t, repo, "rev-parse", "release")
	for _, method := range []domain.MergeMethod{"merge", "squash"} {
		if _, err := mergeInto(m, repo, "release", "agent/x", method, "x", false); errs.Code(err) != domain.MergeAlreadyMerged {
			t.Errorf("%s twice: %v", method, err)
		}
	}
	if gitTrim(t, repo, "rev-parse", "release") != tip {
		t.Error("a refusal moved the target")
	}
}

func TestMergeInto_ACheckedOutTargetFollowsWithItsFiles(t *testing.T) {
	repo := mergeRepo(t)
	if _, err := mergeInto(NewMerger(), repo, "main", "agent/x", domain.MergeMethodSquash, "squashed", true); err != nil {
		t.Fatal(err)
	}
	if gitTrim(t, repo, "log", "-1", "--format=%s", "main") != "squashed" || gitTrim(t, repo, "status", "--porcelain") != "" {
		t.Fatal("main not squashed or checkout not clean")
	}
	if b, err := os.ReadFile(filepath.Join(repo, "agent.txt")); err != nil || string(b) != "a\n" {
		t.Fatalf("agent.txt = %q, %v", b, err)
	}
	if gitTrim(t, repo, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Fatal("HEAD left main")
	}
}

func TestMergeInto_ACheckedOutTargetNeverOverwritesALocalChange(t *testing.T) {
	repo := mergeRepo(t)
	put(t, repo, "agent.txt", "mine, untracked\n")
	tip := gitTrim(t, repo, "rev-parse", "main")
	_, err := mergeInto(NewMerger(), repo, "main", "agent/x", domain.MergeMethodSquash, "s", true)
	if errs.Code(err) != domain.MergeFailed {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "agent.txt")); string(b) != "mine, untracked\n" || gitTrim(t, repo, "rev-parse", "main") != tip {
		t.Fatal("the checkout or main changed")
	}
}

func TestMerger_BranchesAheadAndSubject(t *testing.T) {
	repo := mergeRepo(t)
	gitIn(t, repo, "branch", "release", "main")
	m := NewMerger()
	bl, err := m.Branches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(bl.Names)
	if !slices.Equal(bl.Names, []string{"agent/x", "main", "release"}) || bl.Default != "main" {
		t.Fatalf("branches = %+v", bl)
	}
	if n, err := m.Ahead(context.Background(), repo, "release", "agent/x"); err != nil || n != 1 {
		t.Fatalf("ahead = %d, %v", n, err)
	}
	if n, _ := m.Ahead(context.Background(), repo, "agent/x", "release"); n != 0 {
		t.Fatalf("ahead the other way = %d", n)
	}
	if s, err := m.Subject(context.Background(), repo, "agent/x"); err != nil || s != "agent work" {
		t.Fatalf("subject = %q, %v", s, err)
	}
	// With no main, master is the default; with neither, none is.
	gitIn(t, repo, "branch", "-m", "main", "master")
	if bl, _ := m.Branches(context.Background(), repo); bl.Default != "master" {
		t.Fatalf("default = %q", bl.Default)
	}
	gitIn(t, repo, "branch", "-m", "master", "trunk")
	if bl, _ := m.Branches(context.Background(), repo); bl.Default != "" {
		t.Fatalf("default = %q", bl.Default)
	}
}
