//go:build !windows

package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
)

const fakeClaude = `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ] && { echo '{"loggedIn": true}'; exit 0; }
  echo '{"loggedIn": false}'
  exit 1
fi
[ "$1 $2 $3" = "auth login --claudeai" ] || exit 2
echo "If the browser didn't open, visit: https://claude.ai/oauth/authorize?state=abc"
printf "Paste code here if prompted > "
read code
if [ "$code" != "good-code" ]; then
  echo "invalid code"
  exit 1
fi
echo '{"token":"sk-ant-oat01-httpsecret"}' > "$CLAUDE_CONFIG_DIR/.credentials.json"
`

type fixedBin string

func (b fixedBin) Bin() string      { return string(b) }
func (b fixedBin) Installed() bool  { return true }
func (b fixedBin) Overridden() bool { return false }

func waitOAuthPhase(t *testing.T, h *Handler, phase string) string {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		body := getJSON(t, h, "/get_agent_oauth").Body.String()
		if strings.Contains(body, `"phase":"`+phase+`"`) {
			return body
		}
	}
	t.Fatalf("the flow never reached %q", phase)
	return ""
}

func TestTheOAuthFlowIsStartedResumedAnsweredAndCancelledOverHTTPWithoutTheTokenEverShowing(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	cred := agentcredential.New(sqlite.NewAgentCredentialRepository(db), box, nil)
	oauth := agentoauth.New(agentoauth.Deps{Bin: fixedBin(script), ConfigDir: filepath.Join(dir, "home"), PTY: ptyunix.New()})
	t.Cleanup(oauth.Close)
	h := NewHandler(Services{AgentCredential: cred, AgentOAuth: oauth})

	if body := getJSON(t, h, "/get_agent_oauth").Body.String(); !strings.Contains(body, `"phase":"idle"`) {
		t.Fatalf("initial = %s", body)
	}
	if rec := postJSON(t, h, "/start_agent_oauth", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	body := waitOAuthPhase(t, h, "waiting_for_code")
	if !strings.Contains(body, "https://claude.ai/oauth/authorize?state=abc") {
		t.Errorf("no URL in %s", body)
	}
	if rec := postJSON(t, h, "/start_agent_oauth", map[string]any{}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "AGENT_OAUTH_BUSY") {
		t.Errorf("second start = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/submit_agent_oauth_code", map[string]any{"code": " "}); rec.Code != http.StatusBadRequest {
		t.Errorf("empty code = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/submit_agent_oauth_code", map[string]any{"code": "good-code"}); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "httpsecret") {
		t.Fatalf("submit = %d %s", rec.Code, rec.Body)
	}
	body = waitOAuthPhase(t, h, "done")
	if strings.Contains(body, "httpsecret") || strings.Contains(body, "good-code") {
		t.Errorf("state carries a secret: %s", body)
	}
	if got := getJSON(t, h, "/get_agent_credential").Body.String(); !strings.Contains(got, `"set":false`) {
		t.Errorf("the harness stored a credential for the login: %s", got)
	}
	if rec := postJSON(t, h, "/submit_agent_oauth_code", map[string]any{"code": "x"}); rec.Code != http.StatusConflict {
		t.Errorf("submit without a flow = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/cancel_agent_oauth", map[string]any{}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"idle"`) {
		t.Errorf("cancel = %d %s", rec.Code, rec.Body)
	}
}

func TestTheOAuthEndpointsAnswer503WithoutTheFlow(t *testing.T) {
	h := NewHandler(Services{})
	if rec := getJSON(t, h, "/get_agent_oauth"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("get = %d", rec.Code)
	}
}
