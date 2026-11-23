package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func islandFiles() fstest.MapFS {
	return fstest.MapFS{
		"app.js":              {Data: []byte("app")},
		"app.css":             {Data: []byte("css")},
		"manifest.json":       {Data: []byte(`{"islands":{"top-bar":"islands.server.js"},"client":{"js":"islands-3F9A1C2B.js"}}`)},
		"islands.server.js":   {Data: []byte("server")},
		"islands-3F9A1C2B.js": {Data: []byte("client")},
	}
}

func TestTheClientBundleIsNamedAndServedUnderItsHashedName(t *testing.T) {
	a, err := newAssets(islandFiles())
	if err != nil {
		t.Fatal(err)
	}
	if !a.IslandsBuilt() {
		t.Fatal("islands are built")
	}
	url := a.ClientBundleURL()
	if url == "" || url[:len("/static/islands-3F9A1C2B.js")] != "/static/islands-3F9A1C2B.js" {
		t.Fatalf("client bundle url = %q", url)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "client" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestIslandsNotBuiltAreReportedAsNotBuilt(t *testing.T) {
	a, err := newAssets(fstest.MapFS{"app.js": {Data: []byte("a")}, "app.css": {Data: []byte("c")}})
	if err != nil {
		t.Fatal(err)
	}
	if a.IslandsBuilt() || a.ClientBundleURL() != "" {
		t.Fatal("no manifest: islands are not built")
	}
}

func TestAManifestNamingAMissingClientBundleIsNotBuilt(t *testing.T) {
	files := islandFiles()
	delete(files, "islands-3F9A1C2B.js")
	a, _ := newAssets(files)
	if a.IslandsBuilt() {
		t.Fatal("a manifest whose client bundle is missing is not built")
	}
}

func TestTheClientStylesheetIsLinkedAndHashedWhenTheManifestNamesIt(t *testing.T) {
	files := islandFiles()
	files["manifest.json"] = &fstest.MapFile{Data: []byte(`{"islands":{"top-bar":"islands.server.js"},"client":{"js":"islands-3F9A1C2B.js","css":"islands-3F9A1C2B.css"}}`)}
	files["islands-3F9A1C2B.css"] = &fstest.MapFile{Data: []byte("body{}")}
	a, err := newAssets(files)
	if err != nil {
		t.Fatal(err)
	}
	url := a.ClientCSSURL()
	if !strings.HasPrefix(url, "/static/islands-3F9A1C2B.css?v=") {
		t.Fatalf("client stylesheet url = %q", url)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "body{}" || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("status %d body %q cache %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}

func TestNoClientStylesheetIsLinkedWhenTheManifestHasNoneOrItsFileIsMissing(t *testing.T) {
	a, _ := newAssets(islandFiles())
	if a.ClientCSSURL() != "" {
		t.Fatalf("manifest without css: url = %q", a.ClientCSSURL())
	}
	files := islandFiles()
	files["manifest.json"] = &fstest.MapFile{Data: []byte(`{"islands":{},"client":{"js":"islands-3F9A1C2B.js","css":"gone.css"}}`)}
	b, _ := newAssets(files)
	if b.ClientCSSURL() != "" || !b.IslandsBuilt() {
		t.Fatalf("missing css file: url = %q, built = %v", b.ClientCSSURL(), b.IslandsBuilt())
	}
}

func TestAPageLinksTheClientStylesheetBeforeTheScripts(t *testing.T) {
	files := islandFiles()
	files["manifest.json"] = &fstest.MapFile{Data: []byte(`{"islands":{"top-bar":"islands.server.js"},"client":{"js":"islands-3F9A1C2B.js","css":"islands-3F9A1C2B.css"}}`)}
	files["islands-3F9A1C2B.css"] = &fstest.MapFile{Data: []byte("body{}")}
	a, err := newAssets(files)
	if err != nil {
		t.Fatal(err)
	}
	w := board()
	h := NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, Now: func() time.Time { return now },
	}, a, nil)
	body := get(t, h, "/projects/p1").Body.String()
	css := strings.Index(body, `<link rel="stylesheet" href="/static/islands-3F9A1C2B.css?v=`)
	js := strings.Index(body, `<script type="module" src="/static/islands-3F9A1C2B.js?v=`)
	if css < 0 || js < 0 || css > js {
		t.Errorf("the page links the client stylesheet before its script: css at %d, js at %d", css, js)
	}
}
