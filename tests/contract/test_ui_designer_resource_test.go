package contract

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/application/blueprint"
	"operators-mcp/tests/testhelper"
)

func TestReadDesignerResource_ProductionWithEmbed_Success(t *testing.T) {
	html := `<html><body>Designer</body></html>`
	testFS := &staticFS{files: map[string]string{"static/index.html": html}}
	svc := blueprint.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	baseURL, cleanup := testhelper.StartMCPServerWithDesigner(t, svc, false, testFS, "")
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
		t.Fatal("expected at least one content")
	}
	text := testhelper.ResourceResultText(res)
	if text != html {
		t.Errorf("Text = %q, want %q", text, html)
	}
}

func TestReadDesignerResource_ProductionAssetsMissing_StructuredError(t *testing.T) {
	// Empty FS (no static/index.html) so DesignerContent returns assets-missing error.
	emptyFS := &staticFS{files: map[string]string{}}
	svc := blueprint.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	baseURL, cleanup := testhelper.StartMCPServerWithDesigner(t, svc, false, emptyFS, "")
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()
	req := mcplib.ReadResourceRequest{}
	req.Params.URI = ui.DesignerURI
	_, err := c.ReadResource(ctx, req)
	if err == nil {
		t.Fatal("expected error when assets missing")
	}
	if err.Error() == "" {
		t.Error("error should have a message")
	}
}

func TestReadDesignerResource_DevModeWithServerRunning_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html><html><body>Vite dev</body></html>`))
	})
	srv := &http.Server{Addr: ":5174", Handler: mux}
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Close() })
	time.Sleep(50 * time.Millisecond)

	svc := blueprint.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	baseURL, cleanup := testhelper.StartMCPServerWithDesigner(t, svc, true, nil, "http://localhost:5174")
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
		t.Fatal("expected HTML content from proxy")
	}
	text := testhelper.ResourceResultText(res)
	if !strings.Contains(text, "Vite dev") {
		t.Errorf("expected proxied content, got %q", text[:min(80, len(text))])
	}
}

func TestReadDesignerResource_DevModeServerNotRunning_StructuredError(t *testing.T) {
	svc := blueprint.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	baseURL, cleanup := testhelper.StartMCPServerWithDesigner(t, svc, true, nil, "http://127.0.0.1:59999")
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()
	req := mcplib.ReadResourceRequest{}
	req.Params.URI = ui.DesignerURI
	_, err := c.ReadResource(ctx, req)
	if err == nil {
		t.Fatal("expected error when dev server not running")
	}
	if !strings.Contains(err.Error(), "unreachable") && !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected DEV_SERVER_UNREACHABLE-style message, got: %v", err)
	}
}

// staticFS is a minimal fs.FS for tests.
type staticFS struct {
	files map[string]string
}

func (s *staticFS) Open(name string) (fs.File, error) {
	body, ok := s.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &staticFile{name: name, content: body}, nil
}

type staticFile struct {
	name    string
	content string
	offset  int64
}

func (f *staticFile) Stat() (fs.FileInfo, error) {
	return &staticInfo{name: f.name, size: int64(len(f.content))}, nil
}

func (f *staticFile) Read(b []byte) (int, error) {
	if f.offset >= int64(len(f.content)) {
		return 0, io.EOF
	}
	n := copy(b, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

func (f *staticFile) Close() error { return nil }

type staticInfo struct {
	name string
	size int64
}

func (i *staticInfo) Name() string       { return i.name }
func (i *staticInfo) Size() int64        { return i.size }
func (i *staticInfo) Mode() fs.FileMode  { return 0444 }
func (i *staticInfo) ModTime() time.Time { return time.Time{} }
func (i *staticInfo) IsDir() bool        { return false }
func (i *staticInfo) Sys() interface{}   { return nil }
