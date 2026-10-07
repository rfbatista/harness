package orchestration

import (
	"context"
	"sync/atomic"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// countingTranscripts answers like fakeTranscripts and counts the checks.
type countingTranscripts struct {
	err   error
	calls atomic.Int32
}

func (c *countingTranscripts) CanResume(dir, id string) error {
	c.calls.Add(1)
	return c.err
}

func TestResumability(t *testing.T) {
	gone := &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "gone"}
	noTranscript := &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "gone"}
	cases := []struct {
		name        string
		ended       bool
		transcripts error
		want        string
	}{
		{"running", false, nil, "SESSION_ALREADY_RUNNING"},
		{"ended", true, nil, ""},
		{"worktree gone", true, gone, "WORKSPACE_MISSING"},
		{"no saved conversation", true, noTranscript, "SESSION_TRANSCRIPT_MISSING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			sess, _ := startInteractive(t, svc, InteractiveRequest{})
			if tc.ended {
				if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
					t.Fatal(err)
				}
			}
			svc.Transcripts = fakeTranscripts{err: tc.transcripts}

			got, err := svc.Get(context.Background(), sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.ResumeBlocked != tc.want || got.Resumable != (tc.want == "") {
				t.Errorf("Get: resumable=%v resume_blocked=%q, want blocked %q", got.Resumable, got.ResumeBlocked, tc.want)
			}
			list, _ := svc.List(context.Background(), ports.SessionFilter{ProjectID: "p1"})
			if len(list) != 1 || list[0].ResumeBlocked != tc.want || list[0].Resumable != (tc.want == "") {
				t.Errorf("List: %+v, want blocked %q", list, tc.want)
			}
		})
	}
}

func TestResumability_HeadlessSession(t *testing.T) {
	svc, _ := newInteractiveService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	got, _ := svc.Get(context.Background(), d.ID)
	if got.Resumable || got.ResumeBlocked != "SESSION_NOT_INTERACTIVE" {
		t.Fatalf("headless: resumable=%v resume_blocked=%q", got.Resumable, got.ResumeBlocked)
	}
}

// Listing must not touch the filesystem for sessions that cannot be resumed
// anyway: only ended interactive ones are checked.
func TestResumability_OnlyEndedSessionsAreChecked(t *testing.T) {
	svc, _ := newInteractiveService(t)
	startInteractive(t, svc, InteractiveRequest{})
	startInteractive(t, svc, InteractiveRequest{})
	ended, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.EndInteractive(context.Background(), ended.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	tr := &countingTranscripts{}
	svc.Transcripts = tr
	if _, err := svc.List(context.Background(), ports.SessionFilter{ProjectID: "p1"}); err != nil {
		t.Fatal(err)
	}
	if n := tr.calls.Load(); n != 1 {
		t.Fatalf("transcript checks = %d, want 1 (the ended session only)", n)
	}
}

// What the interactive calls hand back carries the signal too.
func TestResumability_OnReturnedSessions(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if sess.ResumeBlocked != "SESSION_ALREADY_RUNNING" {
		t.Errorf("started: resume_blocked = %q", sess.ResumeBlocked)
	}
	ended, err := svc.EndInteractive(context.Background(), sess.ID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ended.Resumable || ended.ResumeBlocked != "" {
		t.Errorf("ended: resumable=%v resume_blocked=%q", ended.Resumable, ended.ResumeBlocked)
	}
	resumed, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Resumable || resumed.ResumeBlocked != "SESSION_ALREADY_RUNNING" {
		t.Errorf("resumed: resumable=%v resume_blocked=%q", resumed.Resumable, resumed.ResumeBlocked)
	}
}
