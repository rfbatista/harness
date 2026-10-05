package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

// static holds the bundles `go run ./cmd/webbuild` writes (app.js, app.css
// and their source maps). They are build output, not source: the directory is
// tracked only through .gitkeep, hence the all: prefix.
//
//go:embed all:static
var static embed.FS

// Assets serves the bundles and names them with a content hash, so a page
// can let browsers cache them for a year and still pick up every rebuild.
type Assets struct {
	files  fs.FS
	hashes map[string]string
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
	for _, name := range []string{"app.js", "app.css"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			continue // not built; Built reports it
		}
		sum := sha256.Sum256(data)
		a.hashes[name] = hex.EncodeToString(sum[:6])
	}
	return a, nil
}

// Built reports whether both bundles are present.
func (a *Assets) Built() bool { return len(a.hashes) == 2 }

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
