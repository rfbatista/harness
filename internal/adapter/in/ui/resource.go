package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
)

const DesignerURI = "ui://designer"

// Code for structured errors (FR-007).
const (
	CodeUIAssetsMissing      = "UI_ASSETS_MISSING"
	CodeDevServerUnreachable = "DEV_SERVER_UNREACHABLE"
)

// DesignerContent returns the HTML and MIME type for the designer resource.
// If devMode is true, fetches from devServerURL; otherwise reads from embedFS.
// Used by MCP resource handlers (e.g. mcp-go).
func DesignerContent(ctx context.Context, devMode bool, embedFS fs.FS, devServerURL string) (html string, mime string, err error) {
	if devMode {
		return fetchDevServer(ctx, devServerURL)
	}
	html, err = readEmbeddedIndex(embedFS)
	if err != nil {
		slog.Warn("designer UI assets missing", "code", CodeUIAssetsMissing, "error", err)
		return "", "", fmt.Errorf("designer UI assets not available: %w", err)
	}
	return html, "text/html", nil
}

func readEmbeddedIndex(embedFS fs.FS) (string, error) {
	if embedFS == nil {
		return "", errors.New("embedded FS is nil")
	}
	// Dist embeds "static" so entries are "static/index.html"
	data, err := fs.ReadFile(embedFS, "static/index.html")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
