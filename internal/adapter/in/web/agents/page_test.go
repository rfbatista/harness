package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/shell"
)

type fakeCatalog struct {
	body json.RawMessage
	err  error
}

func (f fakeCatalog) GetAgentCatalog(context.Context) (json.RawMessage, error) { return f.body, f.err }

const catalogJSON = `{"root":"/data/agents","versions":{},"broken":null,
 "agents":[{"name":"a"},{"name":"b"}],"bundles":[],"skills":[{"name":"tdd"}],
 "mcp_servers":[{"name":"github"}],"built_in_tools":[{"name":"Read"},{"name":"Bash"},{"name":"Edit"}]}`

func serve(t *testing.T, h Handler, path string) (int, string) {
	t.Helper()
	h.Layout = func(context.Context, string, string, string) (shell.Frame, error) {
		return shell.Frame{Built: true}, nil
	}
	h.Render = func(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error {
		var buf bytes.Buffer
		if err := page.Render(r.Context(), &buf); err != nil {
			return err
		}
		w.WriteHeader(status)
		_, err := buf.WriteTo(w)
		return err
	}
	rec := httptest.NewRecorder()
	if err := h.Page(rec, httptest.NewRequest("GET", path, nil)); err != nil {
		t.Fatalf("Page(%s): %v", path, err)
	}
	return rec.Code, rec.Body.String()
}

func tagsOf(html, role string) []string {
	return regexp.MustCompile(`<[a-z]+[^>]*role="`+role+`"[^>]*>`).FindAllString(html, -1)
}

func TestThePageHasFiveTabsAndSelectsTheOneTheRouteNames(t *testing.T) {
	for path, selected := range map[string]string{
		"/agents": "agents", "/agents/skills": "skills", "/agents/bundles": "bundles", "/agents/mcp": "mcp", "/agents/tools": "tools",
		"/agents/skills/tdd": "skills", "/agents/ux-designer": "agents", "/agents/mcp/github/edit": "mcp", "/agents/bundles/new": "bundles",
	} {
		_, html := serve(t, Handler{}, path)
		if !strings.Contains(html, `role="tablist"`) {
			t.Fatalf("%s: no tablist", path)
		}
		tabs := tagsOf(html, "tab")
		if len(tabs) != 5 {
			t.Fatalf("%s: %d tabs, want 5", path, len(tabs))
		}
		for _, tab := range tabs {
			id := regexp.MustCompile(`data-tab="([a-z]+)"`).FindStringSubmatch(tab)[1]
			want := id == selected
			if got := strings.Contains(tab, `aria-selected="true"`); got != want {
				t.Errorf("%s: tab %s selected = %v, want %v (%s)", path, id, got, want, tab)
			}
			if got := strings.Contains(tab, `tabindex="0"`); got != want {
				t.Errorf("%s: tab %s is in the tab order = %v, want %v", path, id, got, want)
			}
		}
		if len(tagsOf(html, "tabpanel")) != 5 {
			t.Errorf("%s: want 5 tab panels", path)
		}
		for _, panel := range tagsOf(html, "tabpanel") {
			id := regexp.MustCompile(`id="agents-panel-([a-z]+)"`).FindStringSubmatch(panel)[1]
			if got := strings.Contains(panel, " hidden"); got == (id == selected) {
				t.Errorf("%s: panel %s hidden = %v", path, id, got)
			}
		}
	}
}

func TestTheNewButtonNamesWhatTheTabCreates(t *testing.T) {
	for path, want := range map[string]string{
		"/agents": "New agent", "/agents/skills": "New skill", "/agents/bundles": "New bundle", "/agents/mcp": "New MCP server", "/agents/tools": "",
	} {
		_, html := serve(t, Handler{}, path)
		m := regexp.MustCompile(`(?s)<a[^>]*data-new[^>]*>(.*?)</a>`).FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("%s: no New button", path)
		}
		hidden := regexp.MustCompile(`<a[^>]*data-new[^>]*>`).FindString(html)
		if want == "" {
			if !strings.Contains(hidden, " hidden") {
				t.Errorf("%s: Tools shows a New button: %s", path, hidden)
			}
			continue
		}
		if strings.Contains(hidden, " hidden") || !strings.Contains(m[1], want) {
			t.Errorf("%s: New button = %q (%s), want %q", path, m[1], hidden, want)
		}
	}
}

func TestTheSeedPaintsTheRootAndCounts(t *testing.T) {
	_, html := serve(t, Handler{Catalog: fakeCatalog{body: json.RawMessage(catalogJSON)}}, "/agents")
	if !strings.Contains(html, `id="agent-catalog-seed"`) || !strings.Contains(html, `data-seed="agent-catalog-seed"`) {
		t.Error("the page does not seed the catalog")
	}
	if !strings.Contains(html, "/data/agents") {
		t.Error("the page does not show the catalog root")
	}
	counts := regexp.MustCompile(`data-count="([a-z]+)"[^>]*>(\d*)<`).FindAllStringSubmatch(html, -1)
	got := map[string]string{}
	for _, c := range counts {
		got[c[1]] = c[2]
	}
	want := map[string]string{"agents": "2", "skills": "1", "bundles": "0", "mcp": "1", "tools": "3"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("count %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestWithoutACatalogReadThePageStillRendersAndTheBrowserReadsIt(t *testing.T) {
	for name, h := range map[string]Handler{
		"no reader":     {},
		"failing":       {Catalog: fakeCatalog{err: errors.New("down")}},
		"malformed":     {Catalog: fakeCatalog{body: json.RawMessage(`not json`)}},
		"not an object": {Catalog: fakeCatalog{body: json.RawMessage(`[1]`)}},
	} {
		code, html := serve(t, h, "/agents/skills")
		if code != http.StatusOK {
			t.Errorf("%s: status %d", name, code)
		}
		if strings.Contains(html, `id="agent-catalog-seed"`) {
			t.Errorf("%s: seeded without a catalog", name)
		}
		if !strings.Contains(html, "&lt;data dir&gt;/agents/") {
			t.Errorf("%s: no placeholder root", name)
		}
		if len(tagsOf(html, "tab")) != 5 {
			t.Errorf("%s: no tabs", name)
		}
	}
}

func TestAPathThatIsNoRouteIs404(t *testing.T) {
	h := Handler{Layout: func(context.Context, string, string, string) (shell.Frame, error) { return shell.Frame{}, nil }}
	err := h.Page(httptest.NewRecorder(), httptest.NewRequest("GET", "/agents/tools/read", nil))
	if err == nil || !strings.HasSuffix(errs.Code(err), "_NOT_FOUND") {
		t.Errorf("err = %v, want a *_NOT_FOUND", err)
	}
}

// The skill form is a full page, like the agent editor: the server paints the
// page with the header and the tabs hidden and the form's place held, so there
// is no flash of the library before the browser takes over.
func TestTheSkillFormRoutesTakeThePageLikeTheAgentEditor(t *testing.T) {
	hiddenTag := func(html, marker string) string {
		return regexp.MustCompile(`<[a-z]+[^>]*` + marker + `[^>]*>`).FindString(html)
	}
	for path, form := range map[string]bool{
		"/agents": false, "/agents/skills": false, "/agents/skills/tdd": false, "/agents/new": false,
		"/agents/skills/new": true, "/agents/skills/tdd/edit": true, "/agents/ux/edit": false,
	} {
		_, html := serve(t, Handler{}, path)
		if got := !strings.Contains(hiddenTag(html, "data-skill-form-page"), " hidden"); got != form {
			t.Errorf("%s: the skill form is shown = %v, want %v", path, got, form)
		}
		fullPage := form || path == "/agents/new" || path == "/agents/ux/edit"
		if got := strings.Contains(hiddenTag(html, `role="tablist"`), " hidden"); got != fullPage {
			t.Errorf("%s: the tabs hidden = %v", path, got)
		}
	}
	if got := hiddenTag(func() string { _, h := serve(t, Handler{}, "/agents/skills/new"); return h }(), "data-agent-editor-page"); !strings.Contains(got, " hidden") {
		t.Errorf("the agent editor shows on the skill form: %s", got)
	}
}
