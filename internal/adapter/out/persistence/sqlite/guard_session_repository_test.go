package sqlite

import (
	"context"
	"testing"

	"operators-mcp/internal/domain"
)

func TestARedCountSurvivesAResumeUnlessTheSessionWasEscalated(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewGuardSessionRepository(db)
	ctx := context.Background()
	cfg := domain.GuardConfig{Mode: domain.SessionModeImplement, RetryLimit: 3}
	if err := r.Keep(ctx, "s", cfg); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 2; want++ {
		if n, err := r.CountRed(ctx, "s"); err != nil || n != want {
			t.Fatalf("count = %d, %v, want %d", n, err, want)
		}
	}

	// A resume keeps the configuration it takes again and the count: only a
	// green declaration, or the person's second chance, starts it over.
	cfg.RetryLimit = 5
	if err := r.Keep(ctx, "s", cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.ResumeRed(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.CountRed(ctx, "s"); n != 3 {
		t.Errorf("a plain resume changed the count: next is %d, want 3", n)
	}
	if got, _, _ := r.Config(ctx, "s"); got.RetryLimit != 5 {
		t.Errorf("the resume did not take the new configuration: %+v", got)
	}

	// An escalated session that is resumed is given another chance.
	if err := r.MarkEscalated(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if err := r.ResumeRed(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.CountRed(ctx, "s"); n != 1 {
		t.Errorf("an escalated session's resume left the count at %d, want it started over (next is 1)", n)
	}
	// The second chance is given once: the next plain resume keeps what is counted.
	if err := r.ResumeRed(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.CountRed(ctx, "s"); n != 2 {
		t.Errorf("the escalation was forgiven twice: next is %d, want 2", n)
	}
	if err := r.ResetRed(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.CountRed(ctx, "s"); n != 1 {
		t.Errorf("a green reset left %d", n)
	}
}
