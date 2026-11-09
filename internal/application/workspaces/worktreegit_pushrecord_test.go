package workspaces_test

import (
	"context"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

type memPushes struct{ got []domain.RecordedPush }

func (m *memPushes) RecordPush(_ context.Context, p domain.RecordedPush) error {
	m.got = append(m.got, p)
	return nil
}
func (m *memPushes) PushesOf(context.Context, string) ([]domain.RecordedPush, error) {
	return m.got, nil
}

var _ ports.PushRecords = (*memPushes)(nil)
var _ ports.PushRecords = (*sqlite.RecordedPushRepository)(nil)

func TestAPushIsRecordedWithWhereItWentAndWhoMadeIt(t *testing.T) {
	ctx := context.Background()
	f := portstest.NewWorktreeGitFixture(t)
	rec := &memPushes{}
	svc, at := workspacesWithPushes(t, f, rec)
	head := f.Heads["insync"]

	_, err := svc.PushBranchTo(ctx, ports.PushTarget{
		WorkspaceID: at.Workspaces["insync"], ExpectedHead: head, Remote: "mirror", RemoteBranch: "review/insync",
		RequestID: "req1", By: domain.PushByAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("recorded %d pushes, want 1", len(rec.got))
	}
	got := rec.got[0]
	if got.RequestID != "req1" || got.SessionID != at.Sessions["insync"] || got.RepositoryID != at.RepositoryID ||
		got.Branch != "insync" || got.Remote != "mirror" || got.RemoteBranch != "review/insync" || got.Commit != head ||
		got.By != domain.PushByAgent || got.At.IsZero() {
		t.Errorf("recorded %+v", got)
	}

	// The person's own Push is recorded as theirs, with no request.
	if _, err := svc.PushBranch(ctx, at.Workspaces["nopush"], f.Heads["nopush"]); err != nil {
		t.Fatal(err)
	}
	last := rec.got[len(rec.got)-1]
	if last.By != domain.PushByPerson || last.RequestID != "" || last.Remote != "origin" || last.RemoteBranch != "nopush" {
		t.Errorf("the person's push recorded %+v", last)
	}

	// A push that failed records nothing.
	n := len(rec.got)
	_, err = svc.PushBranchTo(ctx, ports.PushTarget{WorkspaceID: at.Workspaces["insync"], ExpectedHead: head, Remote: "nowhere"})
	if err == nil || len(rec.got) != n {
		t.Errorf("a failed push recorded something: err %v, %d records", err, len(rec.got)-n)
	}
}

func workspacesWithPushes(t *testing.T, f *portstest.WorktreeGitFixture, p ports.PushRecords) (ports.WorktreeGit, portstest.WorktreeGitTarget) {
	svc, at, _ := workspacestest.NewWorktreeGitWithFeed(t, f, workspacestest.WithPushRecords(p))
	return svc, at
}
