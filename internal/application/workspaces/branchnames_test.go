package workspaces_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

func newBranchNames(f *mergeFixture) *workspaces.BranchNames {
	return workspaces.NewBranchNames(sqlite.NewRepositoryRepository(f.db), sqlite.NewTicketRepository(f.db), gitcli.NewBranchNames())
}

func TestCheckBranchName(t *testing.T) {
	f := newMergeFixture(t, "main")
	gitRun(t, f.repoDir, "branch", "feat")
	gitRun(t, f.repoDir, "branch", "feat-2")
	svc := newBranchNames(f)
	refs := gitOut(t, f.repoDir, "for-each-ref", "refs/")

	for _, c := range []struct {
		name, trimmed    string
		valid, available bool
		code, suggestion string
		reasonHas        string
	}{
		{"", "", false, false, "", "", ""},
		{"  my/branch  ", "my/branch", true, true, "", "", ""},
		{"feat", "feat", true, false, domain.BranchExists, "feat-3", "feat"},
		{"main", "main", true, false, domain.BranchExists, "main-2", "main"},
		{"bad name", "bad name", false, false, domain.BranchNameInvalid, "", "spaces"},
		{"a..b", "a..b", false, false, domain.BranchNameInvalid, "", ".."},
	} {
		got, err := svc.CheckBranchName(context.Background(), f.repoID, c.name, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != c.trimmed || got.Valid != c.valid || got.Available != c.available ||
			got.Code != c.code || got.Suggestion != c.suggestion || !strings.Contains(got.Reason, c.reasonHas) {
			t.Errorf("%q: %+v", c.name, got)
		}
		if !strings.HasPrefix(got.Generated, "agent/") {
			t.Errorf("%q: generated %q", c.name, got.Generated)
		}
	}
	if after := gitOut(t, f.repoDir, "for-each-ref", "refs/"); after != refs {
		t.Fatalf("a check changed refs:\n%s\n%s", refs, after)
	}
}

func TestCheckBranchNameGeneratesFromTheTask(t *testing.T) {
	f := newMergeFixture(t, "main")
	id := f.task(domain.TicketStatusReview)
	got, err := newBranchNames(f).CheckBranchName(context.Background(), f.repoID, "", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Generated, "agent/task-") || got.Code != "" || got.Reason != "" {
		t.Fatalf("%+v", got)
	}
}

func TestCheckBranchNameOfAnUnknownRepository(t *testing.T) {
	f := newMergeFixture(t, "main")
	_, err := newBranchNames(f).CheckBranchName(context.Background(), "nope", "x", "")
	if errs.Code(err) != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}
