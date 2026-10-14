package app

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
)

// When the database cannot be read, the session reads answer an error: never
// an empty session list, an empty history, or "session not found".
func TestAFailedSessionReadAnswersAnError(t *testing.T) {
	dir := t.TempDir()
	e := startFreezeE2E(t, dir, filepath.Join(dir, "data.db"))
	ids := e.startSessions(1)
	for len(e.history(ids[0])) < 3 {
		time.Sleep(20 * time.Millisecond)
	}
	if err := e.orch.Stop(e.ctx, ids[0]); err != nil {
		t.Fatal(err)
	}

	if err := sqlite.Close(e.db); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/sessions?project_id=" + e.project,
		"/api/sessions/" + ids[0],
		"/api/sessions/" + ids[0] + "/history",
	} {
		res, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s on an unreadable database = %d, want %d", path, res.StatusCode, http.StatusInternalServerError)
		}
	}
}
