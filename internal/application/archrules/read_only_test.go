package archrules

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/application/repoarch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type sessionsByID map[string]*domain.Session

func (m sessionsByID) Get(id string) *domain.Session { return m[id] }

// noRecords is a repository with no saved architecture, so only the read-only
// rule can answer.
type noRecords struct{}

func (noRecords) Current(context.Context, string) (*repoarch.Current, error) { return nil, nil }

func TestAReadOnlySessionsWritesAndCommitsAreDeniedWithAReasonThatNamesReadOnlyAccess(t *testing.T) {
	wt := t.TempDir()
	s := NewService(Deps{Records: noRecords{}, Sessions: sessionsByID{
		"ro": {ID: "ro", RepositoryID: "r1", WorkingDir: wt, Branch: "agent/work", Access: domain.AccessReadOnly, Joined: true},
		"rw": {ID: "rw", RepositoryID: "r1", WorkingDir: wt, Branch: "agent/work", Access: domain.AccessWrite},
	}})
	check := func(id string, c ports.ArchitectureCheck) ports.ArchitectureVerdict {
		c.SessionID = id
		return s.CheckArchitecture(context.Background(), c)
	}

	for name, c := range map[string]ports.ArchitectureCheck{
		"a write":      {Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{wt + "/a.go"}},
		"an edit":      {Point: domain.PointBeforeTool, Tool: "Edit", Paths: []string{wt + "/a.go"}},
		"a multi-edit": {Point: domain.PointBeforeTool, Tool: "MultiEdit", Paths: []string{wt + "/a.go"}},
		"a commit":     {Point: domain.PointBeforeCommit, Tool: "git commit", Paths: []string{"a.go"}},
		"git commit":   {Point: domain.PointBeforeTool, Tool: "Bash", Command: "git commit -m x"},
		"git add":      {Point: domain.PointBeforeTool, Tool: "Bash", Command: "cd sub && git add -A"},
	} {
		v := check("ro", c)
		if !strings.Contains(v.Deny, "read-only access") || !strings.Contains(v.Deny, "agent/work") {
			t.Errorf("%s: deny %q does not name read-only access and the branch", name, v.Deny)
		}
	}

	for name, c := range map[string]ports.ArchitectureCheck{
		"go test":          {Point: domain.PointBeforeTool, Tool: "Bash", Command: "go test ./..."},
		"git status":       {Point: domain.PointBeforeTool, Tool: "Bash", Command: "git status && git log --oneline -3 && git diff"},
		"a write outside":  {Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{t.TempDir() + "/scratch.txt"}},
		"an edit's report": {Point: domain.PointAfterEdit, Tool: "Edit", Paths: []string{wt + "/a.go"}},
	} {
		if v := check("ro", c); v.Deny != "" {
			t.Errorf("%s was denied: %q", name, v.Deny)
		}
	}

	// A write session is not touched by the read-only rule.
	for _, c := range []ports.ArchitectureCheck{
		{Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{wt + "/a.go"}},
		{Point: domain.PointBeforeCommit, Tool: "git commit", Paths: []string{"a.go"}},
	} {
		if v := check("rw", c); v.Deny != "" {
			t.Errorf("a write session was denied: %q", v.Deny)
		}
	}
}

// askedRecords remembers which repository's record the check read.
type askedRecords struct{ repositories []string }

func (a *askedRecords) Current(_ context.Context, repositoryID string) (*repoarch.Current, error) {
	a.repositories = append(a.repositories, repositoryID)
	return nil, nil
}

func TestACommitInAWorktreeNoSessionIsOnIsCheckedAgainstItsRepositorysRecord(t *testing.T) {
	records := &askedRecords{}
	s := NewService(Deps{Records: records, Sessions: sessionsByID{}})
	v := s.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		Point: domain.PointBeforeCommit, Tool: "git commit", Paths: []string{"a.go"},
		Worktree: &ports.CheckedWorktree{RepositoryID: "r9", ProjectID: "p1", Dir: t.TempDir()},
	})
	if v.Deny != "" || len(records.repositories) != 1 || records.repositories[0] != "r9" {
		t.Errorf("verdict %+v, records read %v; want the worktree's repository read once", v, records.repositories)
	}
	records.repositories = nil
	s.CheckArchitecture(context.Background(), ports.ArchitectureCheck{Point: domain.PointBeforeCommit, Tool: "git commit", Paths: []string{"a.go"}})
	if len(records.repositories) != 0 {
		t.Errorf("a check naming no session and no worktree read %v", records.repositories)
	}
}
