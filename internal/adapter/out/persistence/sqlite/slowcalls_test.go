package sqlite

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// logLines records what is logged.
type logLines struct {
	mu    sync.Mutex
	lines []map[string]string
}

func (l *logLines) Enabled(context.Context, slog.Level) bool { return true }
func (l *logLines) Handle(_ context.Context, r slog.Record) error {
	line := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		line[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	return nil
}
func (l *logLines) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logLines) WithGroup(string) slog.Handler      { return l }

// slowCalls is the slow database call lines logged so far.
func (l *logLines) slowCalls() []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]string
	for _, line := range l.lines {
		if line["msg"] == "slow database call" {
			out = append(out, line)
		}
	}
	return out
}

func openLogged(t *testing.T, path string, opts ...Option) (*gorm.DB, *logLines) {
	t.Helper()
	logs := &logLines{}
	db, err := Open(path, append([]Option{WithLogger(slog.New(logs))}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	logs.mu.Lock()
	logs.lines = nil // what opening logged is not under test
	logs.mu.Unlock()
	return db, logs
}

// A call slower than the threshold logs one line naming the repository call
// and how long it took.
func TestOpenLogsASlowDatabaseCallWithItsOperation(t *testing.T) {
	db, logs := openLogged(t, filepath.Join(t.TempDir(), "data.db"), WithSlowThreshold(time.Nanosecond))

	if _, err := NewSettingsRepository(db).Get("missing"); err != nil {
		t.Fatal(err)
	}

	lines := logs.slowCalls()
	if len(lines) != 1 {
		t.Fatalf("slow call lines = %v, want one", lines)
	}
	line := lines[0]
	if line["operation"] != "SettingsRepository.Get" {
		t.Errorf("operation = %q, want SettingsRepository.Get", line["operation"])
	}
	if d, err := time.ParseDuration(line["duration"]); err != nil || d <= 0 {
		t.Errorf("duration = %q, want a positive duration", line["duration"])
	}
	if line["level"] != "WARN" {
		t.Errorf("level = %q, want WARN", line["level"])
	}
	if line["lock_wait"] != "false" {
		t.Errorf("lock_wait = %q, want false", line["lock_wait"])
	}
}

// With the default threshold, a fast call logs nothing.
func TestOpenDoesNotLogAFastDatabaseCall(t *testing.T) {
	db, logs := openLogged(t, filepath.Join(t.TempDir(), "data.db"))
	repo := NewSettingsRepository(db)

	if err := repo.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get("k"); err != nil {
		t.Fatal(err)
	}
	if lines := logs.slowCalls(); len(lines) != 0 {
		t.Errorf("fast calls logged %v", lines)
	}
}

// holdWriteLock takes the write lock on path from another connection;
// release gives it back.
func holdWriteLock(t *testing.T, path string) (release func()) {
	t.Helper()
	other, err := sql.Open(sqlite.DriverName, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Error(err)
		}
	}
}

// waitedOnTheLock checks that lines are one lock wait, by operation, of
// about the time the lock was held.
func waitedOnTheLock(t *testing.T, lines []map[string]string, operation string) {
	t.Helper()
	if len(lines) != 1 {
		t.Fatalf("slow call lines = %v, want one", lines)
	}
	line := lines[0]
	if line["operation"] != operation {
		t.Errorf("operation = %q, want %s", line["operation"], operation)
	}
	if line["lock_wait"] != "true" {
		t.Errorf("lock_wait = %q, want true", line["lock_wait"])
	}
	if d, err := time.ParseDuration(line["duration"]); err != nil || d < 150*time.Millisecond {
		t.Errorf("duration = %q, want the wait (about 200ms)", line["duration"])
	}
}

// A write that waits on the write lock, held by another connection, past
// the threshold is logged as a lock wait.
func TestOpenLogsALockWaitPastTheThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, logs := openLogged(t, path, WithSlowThreshold(50*time.Millisecond))
	release := holdWriteLock(t, path)

	done := make(chan error, 1)
	go func() { done <- NewSettingsRepository(db).Set("k", "v") }()
	time.Sleep(200 * time.Millisecond)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	waitedOnTheLock(t, logs.slowCalls(), "SettingsRepository.Set")
}

// A transaction that waits on the write lock to begin is logged as a lock
// wait, once.
func TestOpenLogsATransactionWaitingOnTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, logs := openLogged(t, path, WithSlowThreshold(50*time.Millisecond))
	release := holdWriteLock(t, path)

	done := make(chan error, 1)
	go func() {
		done <- db.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&SettingModel{Key: "k", Value: "v"}).Error
		})
	}()
	time.Sleep(200 * time.Millisecond)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	waitedOnTheLock(t, logs.slowCalls(), "TestOpenLogsATransactionWaitingOnTheLock")
}
