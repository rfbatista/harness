//go:build !windows

package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/sandboxsettings"
	"path/filepath"
)

const homeExposedLine = "The agent home is inside a git work tree and not ignored: add it to .gitignore."

func isolationPageBody(t *testing.T, exposed func() bool) string {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	srv := httptest.NewServer(web.NewHandler(web.Deps{Projects: cat.Projects, Settings: cat.Settings,
		SandboxSettings: &sandboxsettings.Store{Settings: cat.Settings, Home: "/home/ana"}, AgentHomeExposed: exposed}, builtAssets(t), nil))
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/agent-isolation")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func TestTheCredentialSectionWarnsWhenTheAgentHomeIsInAnUnignoredWorkTree(t *testing.T) {
	if body := isolationPageBody(t, func() bool { return true }); !strings.Contains(body, homeExposedLine) {
		t.Errorf("no warning on the page")
	}
	if body := isolationPageBody(t, func() bool { return false }); strings.Contains(body, "not ignored") {
		t.Errorf("warning shown for a home that is ignored or outside any work tree")
	}
	if body := isolationPageBody(t, nil); strings.Contains(body, "not ignored") {
		t.Errorf("warning shown without a check")
	}
}
