package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// codePages serves the web client over the code fixture, read in process.
type codePages struct {
	h    http.Handler
	f    *portstest.CodeFixture
	at   portstest.CodeTarget
	code ports.CodeBrowser
	base string
}

func newCodePages(t *testing.T, with func(c *codePages, d *Deps)) *codePages {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	c := &codePages{f: portstest.NewCodeFixture(t)}
	code, at := workspacestest.NewCode(t, c.f)
	c.code, c.at = code, at
	c.base = "/projects/" + c.f.ProjectID + "/repositories/" + at.RepositoryID + "/code"
	d := Deps{
		Projects:     fakeProjects{[]*domain.Project{{ID: c.f.ProjectID, Name: "fixture"}}},
		Tasks:        fakeTickets{},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{[]*domain.Repository{{ID: at.RepositoryID, ProjectID: c.f.ProjectID, Name: "fixture", RootDir: c.f.RepoDir}}},
		Code:         code,
	}
	if with != nil {
		with(c, &d)
	}
	c.h = NewHandler(d, assets, nil)
	return c
}

// ssr is the server's first paint: the [data-ssr] part of the page.
func ssr(body string) string {
	i := strings.Index(body, "data-ssr")
	if i < 0 {
		return ""
	}
	return body[i:]
}

func TestRepositoryCodePagePaintsTheTreeAndAFileBeforeAnyScript(t *testing.T) {
	c := newCodePages(t, nil)
	rec := get(t, c.h, c.base+"/files/internal/app/app.go")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d\n%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	paint := ssr(body)
	for _, want := range []string{
		`class="code-source-bar"`, `aria-current="page">Files`, `<span class="branch" title="main">main</span>`,
		`class="file-tree" role="tree"`, `title="internal/app/app.go"`, `aria-selected="true"`,
		`<nav class="crumbs" aria-label="Path">`, `<a aria-current="page">app.go</a>`,
		`class="code-lines" style="--digits:1;"`, `<div class="line" id="L1">`, `aria-label="Line 1"`, `<span class="text">package app</span>`,
		`<span class="zone-tag" data-hue="0" title="Zone · Core context">Adapters</span>`,
	} {
		if !strings.Contains(paint, want) {
			t.Errorf("first paint lacks %s", want)
		}
	}
	seed := seedOf(t, body, "code-seed")
	if seed["file"].(map[string]any)["path"] != "internal/app/app.go" || len(seed["trees"].([]any)) != 3 {
		t.Errorf("seed = file %v, %d trees", seed["file"], len(seed["trees"].([]any)))
	}
	if !strings.Contains(body, `x-data="codePage"`) || !strings.Contains(body, `class="code-layout" x-ref="layout"`) {
		t.Error("the live markup is missing")
	}
}

func TestRepositoryCodePageAtAnotherRefSaysWhenThePathIsNotThere(t *testing.T) {
	c := newCodePages(t, nil)
	rec := get(t, c.h, c.base+"/files/side.txt?ref="+c.f.Root)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	paint := ssr(rec.Body.String())
	if !strings.Contains(paint, "side.txt is not on "+c.f.Root) || !strings.Contains(paint, "PATH_NOT_FOUND") {
		t.Errorf("no PATH_NOT_FOUND banner:\n%s", paint[:min(len(paint), 2000)])
	}
	if !strings.Contains(paint, `title="README.md"`) {
		t.Error("the tree root is not shown under the banner")
	}
}

func TestRepositoryCodePageRefThatDoesNotResolve(t *testing.T) {
	c := newCodePages(t, nil)
	paint := ssr(get(t, c.h, c.base+"?ref=v2").Body.String())
	if !strings.Contains(paint, "v2 is not a branch or tag here") || !strings.Contains(paint, "REF_NOT_FOUND") {
		t.Errorf("paint:\n%s", paint[:min(len(paint), 1500)])
	}
}

func TestRepositoryCodeMapPaintsContextsAndUnzonedFiles(t *testing.T) {
	c := newCodePages(t, nil)
	paint := ssr(get(t, c.h, c.base+"/map").Body.String())
	for _, want := range []string{`class="code-layout" data-side="none"`, `class="code-map"`, `data-kind="context" data-hue="0">Core</span>`, "No bounded context", "Unzoned files", `aria-current="page">Map`} {
		if !strings.Contains(paint, want) {
			t.Errorf("map lacks %s", want)
		}
	}
}

func TestProjectCodeOpensItsOnlyRepositoryAndTheRailLinksIt(t *testing.T) {
	c := newCodePages(t, nil)
	rec := get(t, c.h, "/projects/"+c.f.ProjectID+"/code")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != c.base {
		t.Fatalf("got %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	page := get(t, c.h, c.base).Body.String()
	if !strings.Contains(page, `href="/projects/`+c.f.ProjectID+`/code" aria-current="page"`) {
		t.Error("the rail's Code entry is not current on a code page")
	}
}

func TestWithoutTheCodeBrowserThereIsNoCodePageNorRailEntry(t *testing.T) {
	c := newCodePages(t, func(_ *codePages, d *Deps) { d.Code = nil })
	if rec := get(t, c.h, c.base); rec.Code == http.StatusOK {
		t.Errorf("code page served without a code browser: %d", rec.Code)
	}
	board := get(t, c.h, "/projects/"+c.f.ProjectID).Body.String()
	if strings.Contains(board, "/code\"") {
		t.Error("the rail links Code without a code browser")
	}
}

// unavailableCode answers UNAVAILABLE, as a server started without it would.
type unavailableCode struct{ ports.CodeBrowser }

func (unavailableCode) CodeTree(context.Context, ports.CodeSourceQuery, string) (*domain.CodeTree, error) {
	return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "off"}
}
func (unavailableCode) CodeMap(context.Context, ports.CodeSourceQuery) (*domain.CodeMap, error) {
	return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "off"}
}

func TestCodePageWhenCodeBrowsingIsOff(t *testing.T) {
	c := newCodePages(t, func(_ *codePages, d *Deps) { d.Code = unavailableCode{} })
	paint := ssr(get(t, c.h, c.base).Body.String())
	if !strings.Contains(paint, "Code browsing is off on this server") || !strings.Contains(paint, "UNAVAILABLE") {
		t.Errorf("paint:\n%s", paint[:min(len(paint), 1500)])
	}
}
