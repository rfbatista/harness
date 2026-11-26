package critic

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// blockingDiff holds a launch in its diff read until the launch's context ends.
type blockingDiff struct {
	fakeDiff
	entered chan struct{}
	ctxDone atomic.Bool
	panics  bool
}

func (d *blockingDiff) Diff(ctx context.Context, _ *domain.Session, _ string) (string, error) {
	if d.panics {
		panic("diff exploded")
	}
	close(d.entered)
	<-ctx.Done()
	d.ctxDone.Store(true)
	return "", ctx.Err()
}

func greenRun() ports.ProjectChange {
	c := finished(domain.GateTriggerDeclaration, domain.GateRunGreen)
	return ports.ProjectChange{GateRun: &c}
}

func TestStopCancelsAnInFlightLaunchAndWaitsForIt(t *testing.T) {
	diff := &blockingDiff{fakeDiff: fakeDiff{head: "h"}, entered: make(chan struct{})}
	st := &fakeStarter{}
	svc := New(Deps{Sessions: fakeSessions{}, Settings: fakeSettings{}, Gated: fakeGate{}, Reviews: &fakeReviews{}, Diff: diff, Starter: st})
	svc.ProjectChanged(greenRun())
	<-diff.entered

	stopped := make(chan struct{})
	go func() { svc.Stop(context.Background()); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return: the launch's context was not cancelled")
	}
	if !diff.ctxDone.Load() {
		t.Error("Stop returned before the in-flight launch ended")
	}
	if len(st.reqs) != 0 {
		t.Errorf("a cancelled launch started a critic: %d", len(st.reqs))
	}
	// A change told after Stop launches nothing.
	svc.ProjectChanged(greenRun())
	time.Sleep(50 * time.Millisecond)
	if len(st.reqs) != 0 {
		t.Errorf("a launch after Stop started a critic")
	}
}

func TestAPanicInALaunchIsRecoveredAndItsClaimReleased(t *testing.T) {
	reviews := &fakeReviews{}
	diff := &blockingDiff{fakeDiff: fakeDiff{head: "h"}, entered: make(chan struct{}), panics: true}
	svc := New(Deps{Sessions: fakeSessions{}, Settings: fakeSettings{}, Gated: fakeGate{}, Reviews: reviews, Diff: diff, Starter: &fakeStarter{}})
	svc.ProjectChanged(greenRun()) // the panic must not take the process down
	svc.Stop(context.Background())
	if seen, _ := reviews.Seen(context.Background(), "author", "base0000", "h"); seen {
		t.Error("a launch that panicked left its diff claimed, so no later run reviews it")
	}
}
