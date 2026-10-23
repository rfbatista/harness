package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/agentcredential"
)

func TestTheAgentCredentialIsSetReplacedAndRemovedWithoutEverBeingSentBack(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Services{AgentCredential: agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)})

	rec := getJSON(t, h, "/get_agent_credential")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"set":false`) {
		t.Fatalf("initial = %d %s", rec.Code, rec.Body)
	}
	rec = postJSON(t, h, "/set_agent_credential", map[string]any{"kind": "oauth", "value": "sk-ant-oat01-hush"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"set":true`) || !strings.Contains(rec.Body.String(), `"kind":"oauth"`) {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}
	rec = postJSON(t, h, "/set_agent_credential", map[string]any{"kind": "api_key", "value": "sk-ant-api03-hush"})
	if !strings.Contains(rec.Body.String(), `"kind":"api_key"`) {
		t.Fatalf("replace = %d %s", rec.Code, rec.Body)
	}
	if got := getJSON(t, h, "/get_agent_credential").Body.String(); strings.Contains(got, "hush") {
		t.Fatalf("a response carries the value: %s", got)
	}
	rec = postJSON(t, h, "/set_agent_credential", map[string]any{"kind": "oauth", "value": " "})
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "hush") {
		t.Fatalf("empty value = %d %s", rec.Code, rec.Body)
	}
	rec = postJSON(t, h, "/remove_agent_credential", map[string]any{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"set":false`) {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
}
