package ui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultDevServerURL is the default Vite dev server URL for --dev mode.
const DefaultDevServerURL = "http://localhost:5173"

// fetchDevServer fetches the root HTML from the dev server (e.g. Vite).
func fetchDevServer(ctx context.Context, baseURL string) (html string, mime string, err error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	mime = resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "text/html"
	}
	return string(body), mime, nil
}
