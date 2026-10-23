package sqlite_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/ports"
)

func TestTheAgentCredentialIsNotPlaintextInTheDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)
	const secret = "sk-ant-oat01-plainly-visible-secret"
	if _, err := svc.Set(ports.AgentCredentialOAuth, secret); err != nil {
		t.Fatal(err)
	}

	var rows []map[string]any
	if err := db.Raw("SELECT * FROM agent_credential").Scan(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("rows %v, err %v", rows, err)
	}
	if strings.Contains(fmt.Sprintf("%v %s", rows[0], rows[0]["sealed"]), "plainly-visible") {
		t.Fatalf("the stored row holds the value: %v", rows[0])
	}
	var settings []map[string]any
	_ = db.Raw("SELECT * FROM settings").Scan(&settings)
	if strings.Contains(fmt.Sprint(settings), "plainly-visible") {
		t.Fatal("the value leaked into settings")
	}
	if got := svc.Env(); len(got) != 1 || got[0] != "CLAUDE_CODE_OAUTH_TOKEN="+secret {
		t.Fatalf("env after reading it back: %v", got)
	}
}
