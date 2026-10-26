package sqlite

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func sessionAgentID(t *testing.T, db *gorm.DB, sessionID string) string {
	t.Helper()
	var got string
	if err := db.Raw("SELECT agent_id FROM sessions WHERE id = ?", sessionID).Scan(&got).Error; err != nil {
		t.Fatalf("read session %s: %v", sessionID, err)
	}
	return got
}

// Sessions started before the agent catalog (ADR 0030) keep the ids of the
// agents they ran with: "cfg:<name>" from the dotfiles config, or the row id
// of an agent in the agents table. The catalog knows agents by name, so
// opening the database rewrites those ids to the names the catalog import
// gave the same agents, and the sessions resume again.
func TestOpenRewritesSessionAgentIDsToCatalogNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db := openFile(t, path)
	mustExec(t, db, "INSERT INTO agents (id, name) VALUES ('3d9cbd697cb8051d', 'Golang Agent')")
	mustExec(t, db, "INSERT INTO sessions (id, agent_id) VALUES ('from-config', 'cfg:go-developer'), ('from-table', '3d9cbd697cb8051d'), ('already-named', 'go-developer'), ('no-agent', '')")
	if err := Close(db); err != nil {
		t.Fatal(err)
	}

	db = openFile(t, path)
	want := map[string]string{
		"from-config":   "go-developer",
		"from-table":    "golang-agent",
		"already-named": "go-developer",
		"no-agent":      "",
	}
	for id, agent := range want {
		if got := sessionAgentID(t, db, id); got != agent {
			t.Errorf("session %s: agent_id = %q, want %q", id, got, agent)
		}
	}
}
