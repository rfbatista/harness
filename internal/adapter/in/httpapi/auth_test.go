package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/projects"
)

func tokenRouter(t *testing.T, opts ...Option) http.Handler {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := projects.NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
	return NewRouter(NewHandler(Services{Projects: svc}), opts...)
}

func call(h http.Handler, method, path, auth string) (int, string) {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:5555"
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Code
}

func TestToken(t *testing.T) {
	h := tokenRouter(t, WithToken("s3cret"))
	for name, tc := range map[string]struct {
		method, path, auth string
		status             int
		code               string
	}{
		"health is open":             {"GET", "/api/health", "", 200, ""},
		"the hook route is open":     {"POST", InteractiveSessionStartedPath + "?session_id=x", "", 200, ""},
		"no token":                   {"GET", "/api/list_projects", "", 401, "UNAUTHORIZED"},
		"wrong token":                {"GET", "/api/list_projects", "Bearer nope", 401, "UNAUTHORIZED"},
		"not a bearer":               {"GET", "/api/list_projects", "s3cret", 401, "UNAUTHORIZED"},
		"right token":                {"GET", "/api/list_projects", "Bearer s3cret", 200, ""},
		"writes need it too":         {"POST", "/api/create_project", "", 401, "UNAUTHORIZED"},
		"the terminal needs it":      {"GET", "/api/sessions/x/terminal", "", 401, "UNAUTHORIZED"},
		"the event feed needs it":    {"GET", "/api/events?project_id=p", "", 401, "UNAUTHORIZED"},
		"artifact views need it too": {"GET", "/api/artifacts/x/view/", "", 401, "UNAUTHORIZED"},
	} {
		t.Run(name, func(t *testing.T) {
			status, code := call(h, tc.method, tc.path, tc.auth)
			if status != tc.status || code != tc.code {
				t.Fatalf("%s %s = %d %q, want %d %q", tc.method, tc.path, status, code, tc.status, tc.code)
			}
		})
	}
}

func TestNoTokenLeavesTheAPIOpen(t *testing.T) {
	if status, _ := call(tokenRouter(t), "GET", "/api/list_projects", ""); status != 200 {
		t.Fatalf("list_projects without a configured token = %d, want 200", status)
	}
}
