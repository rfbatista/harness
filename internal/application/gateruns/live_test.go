package gateruns

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pausingExec prints a first part, waits until released, then prints the rest.
type pausingExec struct {
	printed chan struct{}
	release chan struct{}
}

func (p *pausingExec) Execute(_ context.Context, _ *domain.Session, _ string, _ time.Duration, out io.Writer) ports.GateExit {
	_, _ = io.WriteString(out, "first;")
	close(p.printed)
	<-p.release
	_, _ = io.WriteString(out, "second")
	return ports.GateExit{}
}

type feedSink struct {
	mu      sync.Mutex
	changes []ports.GateRunChange
}

func (f *feedSink) AnnounceChange(c ports.ProjectChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, *c.GateRun)
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestAGatesOutputIsReadableWhileItRunsAndALateReaderGetsTheEarlierPartFirst(t *testing.T) {
	exec := &pausingExec{printed: make(chan struct{}), release: make(chan struct{})}
	sink := &feedSink{}
	svc := New(Deps{
		Sessions: fakeSessions{"s": {ID: "s", ProjectID: "p"}},
		Guard:    fakeGuard{"s": domain.GuardConfig{Mode: domain.SessionModeImplement, Gates: []domain.GuardGate{{Kind: domain.GateBuild, Command: "x"}}}},
		Store:    &memStore{}, Exec: exec, Announce: sink,
	})
	var run *domain.GateRun
	finished := make(chan struct{})
	go func() {
		run, _ = svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson})
		close(finished)
	}()
	<-exec.printed

	// The run id is on the started event; a reader joining now gets "first;" before the gate ends.
	var runID string
	eventually(t, func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		if len(sink.changes) > 0 {
			runID = sink.changes[0].RunID
		}
		return runID != ""
	})
	got := &syncBuffer{}
	streamed := make(chan error, 1)
	go func() { streamed <- svc.Output(context.Background(), "s", runID, domain.GateBuild, got) }()
	eventually(t, func() bool { return got.String() == "first;" })
	select {
	case <-finished:
		t.Fatal("the run ended before it was released")
	default:
	}

	close(exec.release)
	<-finished
	if err := <-streamed; err != nil || got.String() != "first;second" {
		t.Errorf("streamed %q, %v", got.String(), err)
	}

	// After the run, the recorded output answers.
	var again bytes.Buffer
	if err := svc.Output(context.Background(), "s", run.ID, domain.GateBuild, &again); err != nil || again.String() != "first;second" {
		t.Errorf("recorded %q, %v", again.String(), err)
	}
	if err := svc.Output(context.Background(), "s", run.ID, domain.GateLint, &again); err == nil {
		t.Error("a gate that was not in the run answered")
	}
}

func TestARunAnnouncesOneStartAndOneFinishWithTheSessionTriggerAndResult(t *testing.T) {
	sink := &feedSink{}
	svc, exec := runner(domain.SessionModeImplement, nil, false)
	svc.d.Announce = sink
	svc.d.Sessions = fakeSessions{"s": {ID: "s", ProjectID: "p"}}
	exec.exits["lint"] = 1
	run, _ := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerDeclaration})

	if len(sink.changes) != 2 {
		t.Fatalf("changes = %+v", sink.changes)
	}
	start, end := sink.changes[0], sink.changes[1]
	if start.Phase != "started" || start.Result != "" || start.ProjectID != "p" || start.SessionID != "s" || start.Trigger != domain.GateTriggerDeclaration || start.RunID != run.ID {
		t.Errorf("started = %+v", start)
	}
	if end.Phase != "finished" || end.Result != domain.GateRunRed || !end.Counted || end.RunID != run.ID {
		t.Errorf("finished = %+v", end)
	}
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
