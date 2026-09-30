package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/tests/testhelper"
)

func TestUIProxy_DevModeServesFromVite(t *testing.T) {
	// Start a stub HTTP server simulating Vite dev server on 5173.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html><html><head><title>Designer</title></head><body>Dev</body></html>`))
	})
	srv := &http.Server{Addr: ":5173", Handler: mux}
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })

	cat := catalog.New(catalog.Deps{})
	baseURL, cleanup := testhelper.StartMCPServerWithDesigner(t, &cat, true, nil, "http://localhost:5173")
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()
	req := mcplib.ReadResourceRequest{}
	req.Params.URI = ui.DesignerURI
	res, err := c.ReadResource(ctx, req)
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("expected content")
	}
	text := testhelper.ResourceResultText(res)
	if !strings.Contains(text, "Dev") || !strings.Contains(text, "<html") {
		t.Errorf("expected HTML from proxy, got: %s", text[:min(120, len(text))])
	}
}
