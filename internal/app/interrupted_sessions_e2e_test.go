//go:build !windows

package app

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A restart stops every session the server ran (ADR 0078); the ones that were
// mid-turn carry the interrupted mark, and the boot lists them in one inbox
// notification (ADR 0080, ADR 0081).

// startOnTheServer starts a session whose agent the server's own terminal
// host runs, the kind a restart orphans.
func (e *shellE2E) startOnTheServer(taskID string) string {
	e.t.Helper()
	return e.startWith(ports.InteractiveRequest{
		ProjectID: e.project, RepositoryID: e.repo, TicketID: taskID, Mode: "implement",
		RunsOn: domain.RunnerServer, RunnerHost: "laptop", Size: ports.TermSize{Cols: 80, Rows: 24},
	}).id
}

// crash stops the server and puts each session back in the status it was
// stored in when the old server died: a crash, a SIGKILL or a reboot runs no
// shutdown step, so the database still holds what the session was doing.
func (e *shellE2E) crash(stored map[string]domain.SessionStatus) {
	e.t.Helper()
	if err := e.app.Stop(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	e.srv.Close()
	db, err := sqlite.Open(e.cfg.DBPath)
	if err != nil {
		e.t.Fatal(err)
	}
	for id, status := range stored {
		err := db.Exec("UPDATE sessions SET status = ?, interrupted_at_ms = 0 WHERE id = ?", string(status), id).Error
		if err != nil {
			e.t.Fatal(err)
		}
		if err := db.Exec("DELETE FROM session_events WHERE session_id = ? AND type = 'interrupted_by_restart'", id).Error; err != nil {
			e.t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	e.boot()
	e.serve()
}

func (e *shellE2E) interruptedNotifications() []*domain.Notification {
	e.t.Helper()
	page, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []*domain.Notification
	for _, n := range page.Notifications {
		if n.Kind == domain.NotificationSessionsInterrupted {
			out = append(out, n)
		}
	}
	return out
}

func (e *shellE2E) session(id string) *domain.Session {
	e.t.Helper()
	sess, err := e.orch.Get(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return sess
}

func TestACrashDuringATurnAnApprovalWaitAndIdleMarksOnlyTheMidTurnSessions(t *testing.T) {
	e := newShellE2EWith(t, "interrupted.db")
	task := e.task("Ship")
	thinking, waiting, idle := e.startOnTheServer(task), e.startOnTheServer(task), e.startOnTheServer(task)

	e.crash(map[string]domain.SessionStatus{
		thinking: domain.SessionThinking, waiting: domain.SessionWaitingApproval, idle: domain.SessionIdle,
	})

	for id, want := range map[string]bool{thinking: true, waiting: true, idle: false} {
		got := e.session(id)
		if got.Status != domain.SessionStopped || (got.InterruptedAt != nil) != want {
			t.Errorf("session %s = %s marked %v, want stopped and marked %v", id, got.Status, got.InterruptedAt, want)
		}
	}
	notes := e.interruptedNotifications()
	if len(notes) != 1 || len(notes[0].InterruptedSessions) != 2 || notes[0].Title != "2 sessions interrupted by a restart" {
		t.Fatalf("notifications = %+v, want one listing the two interrupted sessions", notes)
	}
	for _, s := range notes[0].InterruptedSessions {
		if s.SessionID == idle || s.State != domain.InterruptedSessionInterrupted || s.TaskTitle != "Ship" {
			t.Errorf("listed session = %+v", s)
		}
	}
}

func TestACleanRestartMarksARunningSessionAndASecondBootListsItAgain(t *testing.T) {
	e := newShellE2EWith(t, "interrupted.db")
	id := e.startOnTheServer(e.task("Ship"))

	e.restart()
	if got := e.session(id); got.Status != domain.SessionStopped || got.InterruptedAt == nil {
		t.Fatalf("after a clean restart the session is %s marked %v", got.Status, got.InterruptedAt)
	}
	first := e.interruptedNotifications()
	if len(first) != 1 || first[0].ResolvedAt != nil {
		t.Fatalf("after the first boot: %+v", first)
	}

	e.restart() // nothing resumed it
	var open []*domain.Notification
	for _, n := range e.interruptedNotifications() {
		if n.ResolvedAt == nil {
			open = append(open, n)
		}
	}
	if len(open) != 1 || open[0].ID == first[0].ID || len(open[0].InterruptedSessions) != 1 {
		t.Fatalf("after the second boot the open notifications = %+v, want one new one", open)
	}
}

func TestABootWithNoInterruptedSessionPostsNoNotification(t *testing.T) {
	e := newShellE2EWith(t, "interrupted.db")
	idle := e.startOnTheServer(e.task("Ship"))
	e.crash(map[string]domain.SessionStatus{idle: domain.SessionIdle})
	if got := e.interruptedNotifications(); len(got) != 0 {
		t.Fatalf("notifications = %+v, want none", got)
	}
}
