package orchestration

import (
	"context"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeTranscriptReader records what it was asked for.
type fakeTranscriptReader struct {
	dir, id string
	q       ports.TranscriptQuery
	err     error
}

func (f *fakeTranscriptReader) ReadTranscript(dir, id string, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	f.dir, f.id, f.q = dir, id, q
	return ports.TranscriptPage{Entries: []ports.TranscriptEntry{{Kind: "user", Text: "hi"}}}, f.err
}

func TestSessionTranscript(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	reader := &fakeTranscriptReader{}
	svc.TranscriptReader = reader
	ctx := context.Background()

	p, err := svc.SessionTranscript(ctx, sess.ID, ports.TranscriptQuery{Before: "1.0"})
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("read = %+v, %v", p, err)
	}
	if reader.dir != sess.WorkingDir || reader.id != sess.ID || reader.q.Limit != ports.TranscriptDefaultLimit || reader.q.Before != "1.0" {
		t.Errorf("reader asked for %q %q %+v", reader.dir, reader.id, reader.q)
	}

	// After /clear the conversation moved: the transcript follows it, as a
	// resume would. Ended sessions read the same way.
	if err := svc.RecordClaudeSession(ctx, sess.ID, "c2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SessionTranscript(ctx, sess.ID, ports.TranscriptQuery{Limit: 500}); err != nil || reader.id != "c2" {
		t.Errorf("after /clear and end: id %q, %v", reader.id, err)
	}

	for _, q := range []ports.TranscriptQuery{{Limit: -1}, {Limit: 501}, {Before: "1.0", After: "2.0", Limit: 10}} {
		if _, err := svc.SessionTranscript(ctx, sess.ID, q); errs.Code(err) != "INVALID_INPUT" {
			t.Errorf("%+v: %v, want INVALID_INPUT", q, err)
		}
	}
	if _, err := svc.SessionTranscript(ctx, "nope", ports.TranscriptQuery{}); errs.Code(err) != "SESSION_NOT_FOUND" {
		t.Errorf("unknown session: %v", err)
	}
	reader.err = &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "none"}
	if _, err := svc.SessionTranscript(ctx, sess.ID, ports.TranscriptQuery{}); errs.Code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("reader error: %v", err)
	}
	svc.TranscriptReader = nil
	if _, err := svc.SessionTranscript(ctx, sess.ID, ports.TranscriptQuery{}); errs.Code(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Errorf("no reader: %v", err)
	}
}

// A headless session's claude saves its conversation under the session id.
func TestSessionTranscript_HeadlessSession(t *testing.T) {
	svc, _ := newInteractiveService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	reader := &fakeTranscriptReader{}
	svc.TranscriptReader = reader
	if _, err := svc.SessionTranscript(context.Background(), d.ID, ports.TranscriptQuery{}); err != nil || reader.id != d.ID {
		t.Errorf("headless: id %q, %v", reader.id, err)
	}
}
