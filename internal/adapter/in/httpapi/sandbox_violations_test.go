package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestHTTP_SessionSandboxViolations(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	get := func(id string) (int, struct {
		Available  bool                     `json:"available"`
		Violations []ports.SandboxViolation `json:"violations"`
	}) {
		resp, err := http.Get(env.srv.URL + "/api/sessions/" + id + "/sandbox/violations")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Available  bool                     `json:"available"`
			Violations []ports.SandboxViolation `json:"violations"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	sess, err := env.sessions.Create(&domain.Session{ID: "s1", ProjectID: "p1", Status: domain.SessionDone})
	if err != nil {
		t.Fatal(err)
	}

	// No violation source, as on Linux.
	if status, out := get(sess.ID); status != http.StatusOK || out.Available || out.Violations == nil || len(out.Violations) != 0 {
		t.Errorf("without a source: %d %+v, want 200, not available, an empty list", status, out)
	}

	env.orch.Sandbox = &orchestration.Sandbox{Violations: &portstest.FakeViolationSource{}, ViolationStore: env.sessions}
	at := time.Date(2026, 10, 23, 21, 19, 2, 0, time.UTC)
	if err := env.sessions.AddSandboxViolation(sess.ID, ports.SandboxViolation{When: at, Operation: "file-read-data", Path: "/Users/me/.ssh/id_ed25519", Process: "git"}); err != nil {
		t.Fatal(err)
	}
	status, out := get(sess.ID)
	if status != http.StatusOK || !out.Available || len(out.Violations) != 1 || out.Violations[0].Path != "/Users/me/.ssh/id_ed25519" || !out.Violations[0].When.Equal(at) {
		t.Errorf("with a source: %d %+v", status, out)
	}
	if status, _ := get("nope"); status != http.StatusNotFound {
		t.Errorf("unknown session = %d, want 404", status)
	}
}
