package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/blueprint"
)

func newMCPHandler(t *testing.T) *Handler {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := blueprint.NewService(
		sqlite.NewProjectRepository(db),
		sqlite.NewRepositoryRepository(db),
		sqlite.NewZoneRepository(db),
		sqlite.NewAgentRepository(db),
		sqlite.NewPromptRepository(db),
		sqlite.NewSkillRepository(db),
		sqlite.NewMCPServerRepository(db),
		sqlite.NewToolRepository(db),
		nil,
		nil,
		"",
	)
	return NewHandler(svc, nil, nil, nil, nil, nil)
}

func TestHTTP_ImportMCPServers(t *testing.T) {
	h := newMCPHandler(t)
	content := `{
		"mcpServers": {
			"github": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"]
			},
			"remote": {
				"url": "https://example.com/mcp"
			}
		}
	}`
	body, _ := json.Marshal(map[string]any{
		"content":      content,
		"on_duplicate": "skip",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/import_mcp_servers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		MCPServers []map[string]any `json:"mcp_servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.MCPServers) != 2 {
		t.Fatalf("want 2 imported servers, got %d", len(out.MCPServers))
	}
}

func TestHTTP_ImportMCPServers_InvalidBody(t *testing.T) {
	h := newMCPHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/import_mcp_servers", bytes.NewReader([]byte(`{`)))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid body, got %d", rec.Code)
	}
}

func TestHTTP_TestMCPServer_NotFound(t *testing.T) {
	h := newMCPHandler(t)
	body, _ := json.Marshal(map[string]any{
		"mcp_server_id": "missing",
		"persist":       false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/test_mcp_server", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for missing server, got %d", rec.Code)
	}
}
