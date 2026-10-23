package orchestration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// violationWorld is a sandbox world whose platform has a violation source the
// test drives.
type violationWorld struct {
	*sandboxWorld
	source *portstest.FakeViolationSource
}

func newViolationWorld(t *testing.T) *violationWorld {
	t.Helper()
	w := newSandboxWorld(t)
	src := &portstest.FakeViolationSource{}
	w.svc.Sandbox.Violations = src
	w.svc.Sandbox.ViolationStore = w.svc.sessions.(ports.SandboxViolationStore)
	return &violationWorld{sandboxWorld: w, source: src}
}

func (w *violationWorld) stored(t *testing.T, id string) []ports.SandboxViolation {
	t.Helper()
	got, err := w.svc.SandboxViolations(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Violations
}

func denied(i int) ports.SandboxViolation {
	return ports.SandboxViolation{When: time.UnixMilli(int64(1_000_000 + i)), Operation: "file-read-data", Path: fmt.Sprintf("/Users/me/.ssh/key%d", i), Process: "git"}
}

func TestSandboxViolations_AreStoredForTheSessionThatWasDenied(t *testing.T) {
	w := newViolationWorld(t)
	a, _ := w.start(t, "implement")
	b, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	w.source.Emit(a.WorkingDir, denied(1))
	eventually(t, "the violation", func() bool { return len(w.stored(t, a.ID)) == 1 })
	got := w.stored(t, a.ID)[0]
	if got.Operation != "file-read-data" || got.Path != "/Users/me/.ssh/key1" || got.Process != "git" || !got.When.Equal(denied(1).When) {
		t.Errorf("stored %+v", got)
	}
	if n := len(w.stored(t, b.ID)); n != 0 {
		t.Errorf("the other session has %d violations", n)
	}
}

func TestSandboxViolations_TheThousandAndFirstEvictsTheOldest(t *testing.T) {
	w := newViolationWorld(t)
	sess, _ := w.start(t, "implement")
	for i := 1; i <= 1001; i++ {
		w.source.Emit(sess.WorkingDir, denied(i))
	}
	eventually(t, "the last violation", func() bool {
		got := w.stored(t, sess.ID)
		return len(got) > 0 && got[0].Path == "/Users/me/.ssh/key1001"
	})
	got := w.stored(t, sess.ID)
	if len(got) != 1000 {
		t.Fatalf("kept %d violations, want 1000", len(got))
	}
	if last := got[len(got)-1]; last.Path != "/Users/me/.ssh/key2" {
		t.Errorf("oldest kept is %q, want key2 (key1 evicted)", last.Path)
	}
}

func TestSandboxViolations_GoWithTheSessionButStayWhenItEnds(t *testing.T) {
	w := newViolationWorld(t)
	sess, _ := w.start(t, "implement")
	w.source.Emit(sess.WorkingDir, denied(1))
	eventually(t, "the violation", func() bool { return len(w.stored(t, sess.ID)) == 1 })

	// The session ending (and, with it, its task being archived: nothing
	// there touches sessions) keeps what was collected.
	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if n := len(w.stored(t, sess.ID)); n != 1 {
		t.Fatalf("after the session ended %d violations, want 1", n)
	}

	if err := w.svc.Delete(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	left, err := w.svc.Sandbox.ViolationStore.SandboxViolations(sess.ID)
	if err != nil || len(left) != 0 {
		t.Errorf("after the session was deleted %d violations remain (err %v)", len(left), err)
	}
}

func TestSandboxViolations_CollectionRunsWhileTheSessionDoesAndStopsAtItsEnd(t *testing.T) {
	w := newViolationWorld(t)
	sess, _ := w.start(t, "implement")
	if !w.source.Following(sess.WorkingDir) {
		t.Fatal("a sandboxed session's violations are not followed")
	}
	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if w.source.Following(sess.WorkingDir) {
		t.Error("still following after the session ended")
	}

	// A resume follows again, under the rules now in force.
	if _, _, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID, RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if !w.source.Following(sess.WorkingDir) {
		t.Error("a resumed session's violations are not followed")
	}
}

func TestSandboxViolations_ASessionWithoutTheSandboxCollectsNone(t *testing.T) {
	w := newViolationWorld(t)
	w.off = true
	sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if w.source.Follows() != 0 {
		t.Error("followed a session that ran without the sandbox")
	}
	got, err := w.svc.SandboxViolations(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || len(got.Violations) != 0 {
		t.Errorf("got %+v, want available and empty", got)
	}
}

func TestSandboxViolations_ThePlatformWithoutASourceSaysCollectionIsNotAvailable(t *testing.T) {
	w := newSandboxWorld(t) // no source, as on Linux
	sess, _ := w.start(t, "implement")
	got, err := w.svc.SandboxViolations(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Available || len(got.Violations) != 0 {
		t.Errorf("got %+v, want not available", got)
	}
}
