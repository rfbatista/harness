package orchestration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
)

const sessionTools = `[{"tool":"read_file","input":{"path":"README.md"},"result":"# Demo"}]`

// recordingWorld is a bench service that follows the sessions of the service
// on the event bus, with a stored task tk1 to run sessions on.
func newRecordingWorld(t *testing.T) *runWorld {
	t.Helper()
	w := newRunWorld(t, readScript)
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	w.sessions.Events = bus
	w.sessions.DefaultEnv = append(w.sessions.DefaultEnv, fakeSessionToolsEnv+"="+sessionTools)
	w.bench = bench.New(bench.Deps{
		Cases:        sqlite.NewBenchCaseRepository(db),
		Scorecards:   sqlite.NewBenchScorecardRepository(db),
		Trajectories: sqlite.NewBenchTrajectoryRepository(db),
		Tasks:        w.sessions.tickets,
		SessionLog:   w.sessions.sessions,
		Settings:     effectiveCritic("opus-critic"),
		Projects:     benchProjects{w.sessions},
		Sessions:     w.sessions,
		ServeMock:    w.serveMock,
		MockName:     "bench",
		Judge:        w.judge,
		Feed:         w.feed,
	})
	w.bench.Subscribe(bus)
	return w
}

// sessionOnTask runs a session on tk1, waits for its tool call to be stored,
// and stops it.
func sessionOnTask(t *testing.T, w *runWorld) string {
	t.Helper()
	ctx := context.Background()
	s, err := w.sessions.Start(ctx, StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "Read the README", TicketID: "tk1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the session's tool result to be stored", func() bool {
		events, _ := w.sessions.sessions.ListEvents(s.ID, 0)
		for _, e := range events {
			if e.Type == "tool_result" {
				return true
			}
		}
		return false
	})
	if err := w.sessions.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func TestASessionOnARecordedTaskRecordsItsCallsAndOneOnAnUnrecordedTaskRecordsNothing(t *testing.T) {
	w := newRecordingWorld(t)
	ctx := context.Background()

	off := false
	if _, err := w.bench.SetTaskRecording(ctx, "p1", "tk1", &off); err != nil {
		t.Fatal(err)
	}
	sessionOnTask(t, w)
	time.Sleep(300 * time.Millisecond) // the end is announced asynchronously
	if list, _ := w.bench.ListTrajectories(ctx, "p1"); len(list) != 0 {
		t.Fatalf("an unrecorded task left %+v", list)
	}

	on := true
	if _, err := w.bench.SetTaskRecording(ctx, "p1", "tk1", &on); err != nil {
		t.Fatal(err)
	}
	id := sessionOnTask(t, w)
	waitFor(t, "the trajectory to be recorded", func() bool {
		_, err := w.bench.GetTrajectory(ctx, "p1", id)
		return err == nil
	})
	got, _ := w.bench.GetTrajectory(ctx, "p1", id)
	if len(got.Steps) != 1 || got.Steps[0].Tool != "read_file" || got.Steps[0].Params["path"] != "README.md" || got.Steps[0].Result != "# Demo" {
		t.Fatalf("trajectory = %+v", got)
	}
}

func TestARecordedTrajectoryReplaysAsACaseAndScoresFullOnToolUse(t *testing.T) {
	w := newRecordingWorld(t)
	ctx := context.Background()
	on := true
	_, _ = w.bench.SetTaskRecording(ctx, "p1", "tk1", &on)
	id := sessionOnTask(t, w)
	waitFor(t, "the trajectory to be recorded", func() bool {
		_, err := w.bench.GetTrajectory(ctx, "p1", id)
		return err == nil
	})
	c, err := w.bench.CreateCaseFromTrajectory(ctx, "p1", id)
	if err != nil {
		t.Fatal(err)
	}

	// The replaying CLI makes the same calls the recording saw.
	w.sessions.DefaultEnv = replaceEnv(w.sessions.DefaultEnv, fakeScriptEnv, `[{"tool":"read_file","params":{"path":"README.md"}}]`)
	run := w.startRun(t)
	card := w.scorecard(t, run)
	if len(card.Cases) != 1 || card.Cases[0].CaseID != c.ID {
		t.Fatalf("scorecard = %+v", card)
	}
	tu := card.Cases[0].ToolUse
	if tu.RightTool.Score != 1 || tu.ValidParams.Score != 1 || tu.RightOrder.Score != 1 {
		b, _ := json.Marshal(card.Cases[0])
		t.Fatalf("tool use = %+v\n%s", tu, b)
	}
}

var _ = domain.BenchSourceTrajectory
