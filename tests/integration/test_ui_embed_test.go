package integration

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"operators-mcp/internal/adapter/in/ui"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/tests/testhelper"
)

// TestUIEmbed_ServerServesDesignerFromEmbed verifies that when the server runs in production
// mode with embedded UI (internal/adapter/in/ui/static populated from web/dist), requesting ui://designer
// returns HTML. Populate static before running: cp -r web/dist/* internal/adapter/in/ui/static/
func TestUIEmbed_ServerServesDesignerFromEmbed(t *testing.T) {
	cat := catalog.New(catalog.Deps{})
	baseURL, cleanup := testhelper.StartMCPServer(t, &cat, false)
	defer cleanup()
	c := testhelper.NewTestClient(t, baseURL)
	defer c.Close()

	ctx := context.Background()
	req := mcplib.ReadResourceRequest{}
	req.Params.URI = ui.DesignerURI
	res, err := c.ReadResource(ctx, req)
	if err != nil {
		t.Fatalf("ReadResource: %v (ensure internal/adapter/in/ui/static is populated: cp -r web/dist/* internal/adapter/in/ui/static/)", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("expected at least one content")
	}
	text := testhelper.ResourceResultText(res)
	if !strings.Contains(text, "<html") && !strings.Contains(text, "<!DOCTYPE") {
		t.Errorf("expected HTML content, got: %s", text[:min(100, len(text))])
	}
}
