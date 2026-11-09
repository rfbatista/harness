package sqlite

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func TestRecordedPushesAreReadBySessionAndByRepositorySince(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecordedPushRepository(db)
	t0 := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for i, p := range []domain.RecordedPush{
		{SessionID: "s1", RepositoryID: "r1", Branch: "a", Remote: "origin", RemoteBranch: "a", Commit: "c1", By: domain.PushByAgent, RequestID: "q1"},
		{SessionID: "s2", RepositoryID: "r1", Branch: "b", Remote: "origin", RemoteBranch: "b", Commit: "c2", By: domain.PushByPerson},
		{SessionID: "s3", RepositoryID: "r2", Branch: "c", Remote: "origin", RemoteBranch: "c", Commit: "c3", By: domain.PushByAgent},
	} {
		p.At = t0.Add(time.Duration(i) * time.Minute)
		if err := r.RecordPush(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.PushesIn(ctx, "r1", t0.Add(time.Second))
	if err != nil || len(got) != 1 || got[0].Branch != "b" || got[0].SessionID != "s2" {
		t.Fatalf("PushesIn since = %+v, %v; want only b from s2", got, err)
	}
	got, _ = r.PushesIn(ctx, "r1", time.Time{})
	if len(got) != 2 || got[0].Branch != "b" || got[1].RequestID != "q1" {
		t.Errorf("PushesIn all = %+v; want newest first, both sessions", got)
	}
	if of, _ := r.PushesOf(ctx, "s1"); len(of) != 1 || of[0].Branch != "a" {
		t.Errorf("PushesOf = %+v", of)
	}
}
