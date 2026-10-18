// Package releases downloads release files over HTTP: the token savers'
// assets and checksum files from the releases the harness pins.
package releases

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"operators-mcp/internal/ports"
)

var _ ports.ReleaseDownloads = (*Client)(nil)

// Client downloads release files. A download has no overall deadline (a
// bundle is tens of megabytes); connecting and the first response byte do.
type Client struct{ http *http.Client }

// NewClient returns a client that follows redirects, as GitHub's release
// downloads answer with one to their storage.
func NewClient() *Client {
	return &Client{http: &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ForceAttemptHTTP2:     true,
	}}}
}

// Download writes the file at url to w.
func (c *Client) Download(ctx context.Context, url string, w io.Writer, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: HTTP 404 from %s", ports.ErrReleaseFileNotFound, url)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d from %s", res.StatusCode, url)
	}
	if progress == nil {
		_, err = io.Copy(w, res.Body)
		return err
	}
	total := res.ContentLength
	var done int64
	progress(0, total)
	buf := make([]byte, 64<<10)
	for {
		n, readErr := res.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			progress(done, total)
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
