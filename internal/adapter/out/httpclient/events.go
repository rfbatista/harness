package httpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.SessionFeed = (*Events)(nil)

// Events is ports.SessionFeed over the server's /api/events stream.
type Events struct{ c *Client }

// NewEvents returns the session feed adapter over c.
func NewEvents(c *Client) *Events { return &Events{c: c} }

// FollowProject follows projectID's session changes until ctx ends or the
// stream does — the server dropped this follower, or the connection was lost
// — and then closes the channel.
func (e *Events) FollowProject(ctx context.Context, projectID string) (<-chan ports.SessionChange, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.c.base+"/api/events?"+url.Values{"project_id": {projectID}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if e.c.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.c.token)
	}
	// A stream lasts as long as the follower wants it, so it must not run
	// under the client's per-request timeout.
	stream := &http.Client{Transport: e.c.http.Transport}
	resp, err := stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coding_pool server at %s: %w", e.c.base, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, decodeError(resp.StatusCode, body)
	}

	changes := make(chan ports.SessionChange, 64)
	go func() {
		defer close(changes)
		defer resp.Body.Close()
		scan := bufio.NewScanner(resp.Body)
		scan.Buffer(make([]byte, 64<<10), 16<<20)
		for scan.Scan() {
			data, ok := strings.CutPrefix(scan.Text(), "data: ")
			if !ok {
				continue // blank separators, ": ping" comments
			}
			var change ports.SessionChange
			if json.Unmarshal([]byte(data), &change) != nil || change.Session == nil {
				continue
			}
			select {
			case changes <- change:
			case <-ctx.Done():
				return
			}
		}
	}()
	return changes, nil
}
