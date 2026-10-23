//go:build !windows

package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/ports"
)

type pastedCredential struct{ set bool }

func (p pastedCredential) Status() ports.AgentCredentialStatus {
	return ports.AgentCredentialStatus{Set: p.set, Kind: ports.AgentCredentialAPIKey}
}
func (p pastedCredential) Set(string, string) (ports.AgentCredentialStatus, error) {
	return p.Status(), nil
}
func (p pastedCredential) Remove() (ports.AgentCredentialStatus, error) { return p.Status(), nil }

func credentialPageBody(t *testing.T, login func() bool, pasted bool) string {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	srv := httptest.NewServer(web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings,
		SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"},
		AgentCredential: pastedCredential{set: pasted}, AgentLogin: login}, builtAssets(t), nil))
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/agent-isolation")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func TestThePageSaysWhichCredentialAgentsUse(t *testing.T) {
	yes, no := func() bool { return true }, func() bool { return false }
	for name, tc := range map[string]struct {
		login  func() bool
		pasted bool
		want   string
	}{
		"login only":  {yes, false, "Agents use your Claude login"},
		"pasted only": {no, true, "Agents use the pasted API key or token"},
		"both":        {yes, true, "Agents use your Claude login; the pasted credential is only a fallback"},
	} {
		body := credentialPageBody(t, tc.login, tc.pasted)
		if !strings.Contains(body, ">"+tc.want+"<") {
			t.Errorf("%s: page lacks %q", name, tc.want)
		}
		if tc.want != "Agents use the pasted API key or token" && strings.Contains(body, ">Agents use the pasted API key or token<") {
			t.Errorf("%s: page also says the pasted credential is in use", name)
		}
	}
	if body := credentialPageBody(t, no, false); strings.Contains(body, "Agents use ") {
		t.Errorf("a line is shown with no credential at all")
	}
	if body := credentialPageBody(t, nil, true); !strings.Contains(body, ">Agents use the pasted API key or token<") {
		t.Errorf("without a login check the pasted credential is the one in use")
	}
}
