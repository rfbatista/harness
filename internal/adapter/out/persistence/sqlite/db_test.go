package sqlite

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"operators-mcp/internal/domain"
)

func openFile(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return db
}

func pragma(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	var v string
	if err := db.Raw("PRAGMA " + name).Scan(&v).Error; err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

// A file database runs in WAL, with an explicit busy timeout and
// synchronous=NORMAL.
func TestOpenFileDatabaseRunsInWAL(t *testing.T) {
	db := openFile(t, filepath.Join(t.TempDir(), "data.db"))
	if got := pragma(t, db, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if got := pragma(t, db, "busy_timeout"); got != "5000" {
		t.Errorf("busy_timeout = %q, want 5000", got)
	}
	if got := pragma(t, db, "synchronous"); got != "1" { // NORMAL
		t.Errorf("synchronous = %q, want 1 (NORMAL)", got)
	}
	if got := pragma(t, Reader(db), "busy_timeout"); got != "5000" {
		t.Errorf("reader pool busy_timeout = %q, want 5000", got)
	}
}

// A write transaction on the writer connection takes the write lock when it
// begins, so no other writer can slip in between its read and its write.
func TestOpenWriteTransactionsTakeTheWriteLockWhenTheyBegin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db := openFile(t, path)
	// No busy timeout on purpose: the app's connections wait busyTimeoutMS for
	// the lock, this one fails at once so the test can see the lock is held.
	other, err := sql.Open(sqlite.DriverName, "file:"+path+"?_busy_timeout=0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	err = db.Transaction(func(tx *gorm.DB) error {
		// Nothing written yet: a deferred transaction would hold no lock.
		_, err := other.Exec("BEGIN IMMEDIATE")
		if err == nil {
			_, _ = other.Exec("ROLLBACK")
			t.Error("another writer began while a write transaction was open")
		} else if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
			t.Errorf("another writer failed with %v, want a lock error", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The reader pool refuses writes; a write through the database handle goes to
// the writer connection, including a raw statement that returns rows.
func TestOpenReaderPoolRefusesWrites(t *testing.T) {
	db := openFile(t, filepath.Join(t.TempDir(), "data.db"))

	if err := Reader(db).Exec("INSERT INTO settings(key, value) VALUES ('a', 'b')").Error; err == nil {
		t.Error("the reader pool accepted a write")
	}

	var key string
	if err := db.Raw("INSERT INTO settings(key, value) VALUES ('c', 'd') RETURNING key").Scan(&key).Error; err != nil || key != "c" {
		t.Fatalf("raw write returning rows = %q, %v", key, err)
	}
	if v, err := NewSettingsRepository(db).Get("c"); err != nil || v != "d" {
		t.Fatalf("the raw write is not stored: %q, %v", v, err)
	}
}

// A read inside a write transaction sees the transaction's own writes: it
// stays on the writer connection.
func TestOpenReadsInsideATransactionSeeItsWrites(t *testing.T) {
	db := openFile(t, filepath.Join(t.TempDir(), "data.db"))
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&SettingModel{Key: "k", Value: "v"}).Error; err != nil {
			return err
		}
		var got SettingModel
		if err := tx.First(&got, "key = ?", "k").Error; err != nil {
			t.Errorf("read inside the transaction: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Opening an existing rollback-journal database switches it to WAL and keeps
// its data.
func TestOpenSwitchesAnExistingRollbackJournalDatabaseToWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db := openFile(t, path)
	if err := NewSettingsRepository(db).Set(domain.SettingWorkspacesRoot, "/w"); err != nil {
		t.Fatal(err)
	}
	if err := Close(db); err != nil {
		t.Fatal(err)
	}
	old, err := sql.Open(sqlite.DriverName, path)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := old.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil || mode != "delete" {
		t.Fatalf("back to rollback journal: %q, %v", mode, err)
	}
	old.Close()

	db = openFile(t, path)
	if got := pragma(t, db, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if v, err := NewSettingsRepository(db).Get(domain.SettingWorkspacesRoot); err != nil || v != "/w" {
		t.Errorf("data after the switch = %q, %v", v, err)
	}
}
