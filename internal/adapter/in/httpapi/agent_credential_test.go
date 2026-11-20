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

func TestAProjectsAgentCredentialIsKeptApartFromTheGlobalOneAndNeverSentBack(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New("", filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil).WithProjects(sqlite.NewProjectAgentCredentialRepository(db))
	h := NewHandler(Services{AgentCredential: svc, ProjectAgentCredentials: svc})

	if rec := postJSON(t, h, "/set_project_agent_credential", map[string]any{"project_id": "p1", "kind": "api_key", "value": "sk-ant-api03-hush"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"api_key"`) {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}
	if got := getJSON(t, h, "/get_project_agent_credential?project_id=p1"); !strings.Contains(got.Body.String(), `"set":true`) || strings.Contains(got.Body.String(), "hush") {
		t.Fatalf("p1 = %d %s", got.Code, got.Body)
	}
	if got := getJSON(t, h, "/get_project_agent_credential?project_id=p2").Body.String(); !strings.Contains(got, `"set":false`) {
		t.Fatalf("p2 reads p1's credential: %s", got)
	}
	if got := getJSON(t, h, "/get_agent_credential").Body.String(); !strings.Contains(got, `"set":false`) {
		t.Fatalf("the global credential changed: %s", got)
	}
	if rec := postJSON(t, h, "/set_project_agent_credential", map[string]any{"kind": "api_key", "value": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("no project = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/remove_project_agent_credential", map[string]any{"project_id": "p1"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"set":false`) {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	if got := svc.EnvFor("p1"); got != nil {
		t.Fatalf("env after removal %v", got)
	}
}

func TestProjectAgentCredentialsAreUnavailableWithoutTheirService(t *testing.T) {
	if rec := getJSON(t, NewHandler(Services{}), "/get_project_agent_credential?project_id=p1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", rec.Code)
	}
}
