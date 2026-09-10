package ui

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
)

// HTTPServer serves the embedded React UI over HTTP.
// Use StartHTTPServer to run it; it serves static assets and falls back to index.html for SPA routing.
type HTTPServer struct {
	Addr   string
	Embed  fs.FS
	server *http.Server
}

// SPAHandler returns an http.Handler that serves the embedded UI from embedFS (e.g. ui.Dist)
// with SPA fallback. Use this to mount the UI under a combined mux (e.g. with MCP at /mcp).
// The embedFS is expected to contain "static/index.html" and "static/assets/*".
func SPAHandler(embedFS fs.FS) (http.Handler, error) {
	if embedFS == nil {
		return http.NotFoundHandler(), nil
	}
	root, err := fs.Sub(embedFS, "static")
	if err != nil {
		return nil, err
	}
	return spaFallback(root), nil
}

// StartHTTPServer starts an HTTP server that serves the embedded UI from embedFS (e.g. ui.Dist).
// The embedFS is expected to contain "static/index.html" and "static/assets/*".
// Cancel ctx to shut down the server gracefully.
func StartHTTPServer(ctx context.Context, addr string, embedFS fs.FS) (*HTTPServer, error) {
	if embedFS == nil {
		return nil, nil
	}
	handler, err := SPAHandler(embedFS)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	srv := &http.Server{Addr: addr, Handler: mux}
	h := &HTTPServer{Addr: addr, Embed: embedFS, server: srv}
	go func() {
		slog.Info("UI HTTP server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("UI HTTP server error", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	return h, nil
}

// spaFallback serves files from root; if the path is not found, serves index.html for SPA client-side routing.
func spaFallback(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		name = name[1:] // strip leading slash
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(root, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html so the client router can handle the path
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
