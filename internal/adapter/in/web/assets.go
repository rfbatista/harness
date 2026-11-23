package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/rfbatista/gohydrate"
)

// static holds the bundles `go run ./cmd/webbuild` writes (app.js, app.css,
// document.css and their source maps). They are build output, not source: the directory is
// tracked only through .gitkeep, hence the all: prefix.
//
//go:embed all:static
var static embed.FS

// Assets serves the bundles and names them with a content hash, so a page
// can let browsers cache them for a year and still pick up every rebuild.
type Assets struct {
	files  fs.FS
	hashes map[string]string
	// client is the React islands' browser bundle, from the manifest gohydrate
	// wrote; "" when `make web` has not built it.
	client   string
	manifest gohydrate.Manifest
}

// NewAssets reads the embedded bundles.
func NewAssets() (*Assets, error) {
	files, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}
	return newAssets(files)
}

func newAssets(files fs.FS) (*Assets, error) {
	a := &Assets{files: files, hashes: map[string]string{}}
	for _, name := range []string{"app.js", "app.css", "document.css"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			continue // not built; Built reports it
		}
		sum := sha256.Sum256(data)
		a.hashes[name] = hex.EncodeToString(sum[:6])
	}
	a.readManifest()
	return a, nil
}

// readManifest names the island client bundle. A missing manifest, or one that
// names a file that is not there, leaves the islands unbuilt.
func (a *Assets) readManifest() {
	data, err := fs.ReadFile(a.files, "manifest.json")
	if err != nil {
		return
	}
	var m gohydrate.Manifest
	if json.Unmarshal(data, &m) != nil || m.Client.JS == "" {
		return
	}
	bundle, err := fs.ReadFile(a.files, m.Client.JS)
	if err != nil {
		return
	}
	sum := sha256.Sum256(bundle)
	a.hashes[m.Client.JS] = hex.EncodeToString(sum[:6])
	a.client = m.Client.JS
	a.manifest = m
}

// IslandsBuilt reports whether the React islands' client bundle is present.
func (a *Assets) IslandsBuilt() bool { return a.client != "" }

// ClientBundleURL is the cache-busting URL of the islands' client bundle, ""
// when it is not built.
func (a *Assets) ClientBundleURL() string {
	if a.client == "" {
		return ""
	}
	return a.URL(a.client)
}

// Built reports whether the app bundles are present. The document
// stylesheet is optional: an HTML document may link it, pages never need it.
func (a *Assets) Built() bool {
	_, js := a.hashes["app.js"]
	_, css := a.hashes["app.css"]
	return js && css
}

// URL is the cache-busting URL of a bundle: /static/app.js?v=<hash>.
func (a *Assets) URL(name string) string {
	if h, ok := a.hashes[name]; ok {
		return "/static/" + name + "?v=" + h
	}
	return "/static/" + name
}

// Handler serves /static/. A versioned request is immutable; anything else
// is revalidated, so a stale hash never sticks.
func (a *Assets) Handler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(a.files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/.") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
