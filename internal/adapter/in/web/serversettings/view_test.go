package serversettings

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
	if !strings.Contains(html, `href="/server" aria-label="Server" title="Server" aria-current="page"`) {
		t.Errorf("the top bar does not mark the Server page as current:\n%s", html)
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
