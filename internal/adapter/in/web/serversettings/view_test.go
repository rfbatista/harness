package serversettings

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"operators-mcp/internal/adapter/in/web/shell"
)

func render(t *testing.T, p Paths) string {
	t.Helper()
	frame := shell.Frame{Title: "Server"}
	frame.Top.ServerHref, frame.Top.OnServer = Href, true
	var b bytes.Buffer
	if err := Page(frame, NewView(p)).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestThePageShowsTheDataDirAndWhatItHolds(t *testing.T) {
	html := render(t, Paths{DataDir: "/home/me/.harness", Source: "the default (~/.harness)", Database: "/home/me/.harness/data.db"})
	for _, want := range []string{
		"/home/me/.harness",
		"Set by the default (~/.harness).",
		"/home/me/.harness/data.db",
		"/home/me/.harness/tools",
		"/home/me/.harness/artifacts",
		"/home/me/.harness/task-resources",
		"/home/me/.harness/git",
		"HARNESS_DATA_DIR",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

func TestAnInMemoryServerHasNoDataDir(t *testing.T) {
	html := render(t, Paths{Database: ":memory:"})
	if strings.Contains(html, "data-data-dir") || strings.Contains(html, "/tools") {
		t.Errorf("an in-memory server shows a data dir:\n%s", html)
	}
	if !strings.Contains(html, "in-memory database") {
		t.Errorf("the page does not say the database is in memory")
	}
}

type settings map[string]string

func (s settings) Setting(key string) string { return s[key] }

func branchBlockSwitches(t *testing.T, stored settings) []string {
	t.Helper()
	h := Handler{
		Paths:    Paths{DataDir: "/home/me/.harness", Source: "a test", Database: ":memory:"},
		Settings: stored,
		Layout:   func(context.Context, string, string, string) (shell.Frame, error) { return shell.Frame{}, nil },
		Render: func(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error {
			return page.Render(r.Context(), w)
		},
	}
	rec := httptest.NewRecorder()
	if err := h.Page(rec, httptest.NewRequest(http.MethodGet, "/server", nil)); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`<input[^>]*data-branch-block[^>]*>`).FindAllString(rec.Body.String(), -1) {
		key := regexp.MustCompile(`data-setting="([^"]*)"`).FindStringSubmatch(m)[1]
		if strings.Contains(m, " checked") {
			key += "=on"
		} else {
			key += "=off"
		}
		out = append(out, key)
	}
	return out
}

func TestTheBranchBlockSectionHasTenSwitchesInTheContractsOrderOnByDefault(t *testing.T) {
	var want []string
	for _, m := range []string{"default", "architect", "implement", "design", "product", "designsystem", "review", "test", "qa", "operate"} {
		want = append(want, "branches.block.mode."+m+"=on")
	}
	if got := branchBlockSwitches(t, settings{}); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("switches = %v, want %v", got, want)
	}
}

func TestAStoredOffTurnsOneNewModesSwitchOff(t *testing.T) {
	got := branchBlockSwitches(t, settings{"branches.block.mode.qa": "off"})
	if len(got) != 10 || got[8] != "branches.block.mode.qa=off" || got[7] != "branches.block.mode.test=on" {
		t.Errorf("switches = %v", got)
	}
}
