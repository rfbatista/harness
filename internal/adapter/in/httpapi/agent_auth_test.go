//go:build !windows

package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/application/agentoauth"
)

// fakeAuthClaude answers `auth status` and `auth logout` from a login file in
// the agent home; the file also holds a token that must never show.
const fakeAuthClaude = `#!/bin/sh
f="$CLAUDE_CONFIG_DIR/.credentials.json"
if [ "$1 $2" = "auth status" ]; then
  [ "$FAKE_STATUS" = broken ] && { echo "boom"; exit 3; }
  if [ -f "$f" ]; then
    echo '{"loggedIn": true, "authMethod": "claude.ai", "subscriptionType": "max", "email": "ana@example.com", "orgId": "org-secret"}'
    exit 0
  fi
  echo '{"loggedIn": false}'
  exit 1
fi
if [ "$1 $2" = "auth logout" ]; then rm -f "$f"; exit 0; fi
exit 2
`

func authRig(t *testing.T) (h *Handler, home string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(fakeAuthClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	auth := agentoauth.NewAuth(agentoauth.AuthDeps{Bin: fixedBin(script), ConfigDir: home})
	return NewHandler(Services{AgentAuth: auth}), home
}

func TestTheAgentSignInStatusIsTheAccountFieldsOnly(t *testing.T) {
	h, home := authRig(t)
	rec := getJSON(t, h, "/get_agent_auth_status")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"signed_in":false,"account":"","subscription":"","auth_method":"","checked_ok":true}` {
		t.Fatalf("signed out = %d %s", rec.Code, rec.Body)
	}
	if err := os.WriteFile(filepath.Join(home, ".credentials.json"), []byte(`{"accessToken":"sk-ant-oat01-filesecret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = getJSON(t, h, "/get_agent_auth_status")
	want := `{"signed_in":true,"account":"ana@example.com","subscription":"max","auth_method":"claude.ai","checked_ok":true}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("signed in = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "filesecret") || strings.Contains(rec.Body.String(), "org-secret") {
		t.Errorf("answer carries more than the account fields: %s", rec.Body)
	}
}

func TestSigningOutRemovesTheLoginAndAnswersTheStatusAfterwards(t *testing.T) {
	h, home := authRig(t)
	if err := os.WriteFile(filepath.Join(home, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, h, "/sign_out_agent_auth", map[string]any{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"signed_in":false`) || !strings.Contains(rec.Body.String(), `"checked_ok":true`) {
		t.Fatalf("sign out = %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(home, ".credentials.json")); err == nil {
		t.Error("the login is still there")
	}
}

func TestAStatusThatCannotBeReadIsNotCheckedRatherThanAnError(t *testing.T) {
	t.Setenv("FAKE_STATUS", "broken")
	h, _ := authRig(t)
	rec := getJSON(t, h, "/get_agent_auth_status")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"checked_ok":false`) || !strings.Contains(rec.Body.String(), `"signed_in":false`) {
		t.Errorf("answer = %d %s", rec.Code, rec.Body)
	}
}

func TestTheAuthEndpointsAnswer503WithoutAnAuth(t *testing.T) {
	h := NewHandler(Services{})
	if rec := getJSON(t, h, "/get_agent_auth_status"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("get = %d", rec.Code)
	}
	if rec := postJSON(t, h, "/sign_out_agent_auth", map[string]any{}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("sign out = %d", rec.Code)
	}
}
