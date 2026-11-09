package sqlite_test

import (
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

func openInterviews(t *testing.T, path string) (*sqlite.InterviewRepository, func()) {
	t.Helper()
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return sqlite.NewInterviewRepository(db), func() { _ = sqlite.Close(db) }
}

func anInterview(id, session string) *domain.Interview {
	return &domain.Interview{
		ID: id, SessionID: session, AgentName: "claude", AskedAt: time.Now(), State: domain.InterviewOpen,
		Questions: []domain.InterviewQuestion{{Title: "Which?", Kind: domain.QuestionSingle, Options: []domain.InterviewOption{{Label: "a"}}}},
	}
}

// A session's feed numbers are kept per session and carry on after the
// database is reopened; a closed interview comes back whole.
func TestInterviewSeqsAreKeptPerSessionAndSurviveReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	repo, closeDB := openInterviews(t, path)
	for _, c := range []struct {
		id, session string
		want        int64
	}{{"a", "s1", 1}, {"b", "s1", 2}, {"c", "s2", 1}} {
		if seq, err := repo.Create(anInterview(c.id, c.session)); err != nil || seq != c.want {
			t.Fatalf("create %s = %d, %v; want seq %d", c.id, seq, err, c.want)
		}
	}
	closeDB()

	repo, closeDB = openInterviews(t, path)
	defer closeDB()
	end, note, delivery := time.Now(), "no", domain.InterviewQueued
	b := anInterview("b", "s1")
	b.State, b.EndedAt, b.DismissNote, b.Delivery = domain.InterviewDismissed, &end, &note, &delivery
	if seq, err := repo.Update(b); err != nil || seq != 3 {
		t.Fatalf("update = %d, %v; want seq 3", seq, err)
	}
	lists, err := repo.List([]string{"s1", "s2", "s3"})
	if err != nil {
		t.Fatal(err)
	}
	if lists["s1"].Seq != 3 || lists["s2"].Seq != 1 || lists["s3"].Seq != 0 {
		t.Errorf("seqs = %d %d %d", lists["s1"].Seq, lists["s2"].Seq, lists["s3"].Seq)
	}
	got := lists["s1"].Interviews
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" || got[1].State != domain.InterviewDismissed ||
		*got[1].DismissNote != "no" || *got[1].Delivery != domain.InterviewQueued || got[1].EndedAt == nil || got[1].Answers != nil {
		t.Errorf("s1 = %+v", got)
	}
	if lists["s3"].Interviews == nil || len(lists["s3"].Interviews) != 0 {
		t.Errorf("an empty session must list [], got %#v", lists["s3"].Interviews)
	}
}

// ExpireQueued is a compare-and-set on a sent or dismissed interview whose
// turn is still queued: an open one, a delivered one and a second call lose
// and take no feed number.
func TestExpireQueuedOnlyWinsWhileTheTurnIsStillQueued(t *testing.T) {
	repo, closeDB := openInterviews(t, filepath.Join(t.TempDir(), "h.db"))
	defer closeDB()
	end, queued, delivered := time.Now(), domain.InterviewQueued, domain.InterviewDelivered
	expired := func(i *domain.Interview) *domain.Interview {
		e := *i
		e.State, e.Delivery = domain.InterviewExpired, nil
		return &e
	}
	sent := func(id string, d *domain.InterviewDelivery) *domain.Interview {
		i := anInterview(id, "s1")
		i.State, i.EndedAt, i.Delivery = domain.InterviewSent, &end, d
		return i
	}
	open, waiting, read := anInterview("open", "s1"), sent("waiting", &queued), sent("read", &delivered)
	for _, i := range []*domain.Interview{open, waiting, read} {
		if _, err := repo.Create(i); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		i   *domain.Interview
		won bool
	}{{open, false}, {read, false}, {waiting, true}, {waiting, false}} {
		won, seq, err := repo.ExpireQueued(expired(c.i))
		if err != nil || won != c.won || (won && seq != 4) || (!won && seq != 0) {
			t.Fatalf("%s: won %v seq %d err %v, want won %v", c.i.ID, won, seq, err, c.won)
		}
	}
	if got, _ := repo.Get("waiting"); got.State != domain.InterviewExpired || got.Delivery != nil {
		t.Errorf("waiting: %+v", got)
	}
	if got, _ := repo.Get("read"); got.State != domain.InterviewSent || *got.Delivery != domain.InterviewDelivered {
		t.Errorf("read: %+v", got)
	}
}
