package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
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
