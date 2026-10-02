// Package httpclient implements the application's driving ports over the
// coding_pool HTTP API. tui-client binds these adapters where the server binds
// the application services, so a screen holds a ports.TicketBoard and cannot
// tell which one it has.
//
// Errors keep their contract across the wire: a response carrying a code
// comes back as *domain.StructuredError with that code, so errs.Code answers
// the same on both sides. A response without one is a *StatusError.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"operators-mcp/internal/domain"
)

// Client is the connection the adapters share: the server's base URL, the
// bearer token when the server requires one, and the HTTP client.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithToken sends token as a bearer token on every request.
func WithToken(token string) Option { return func(c *Client) { c.token = token } }

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a client for the server at base, e.g. http://localhost:8080.
func New(base string, opts ...Option) *Client {
	c := &Client{
		base: strings.TrimRight(base, "/"),
		// Starting a session runs `git worktree add`, which can take a while
		// on a large repository.
		http: &http.Client{Timeout: 2 * time.Minute},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Health reports whether the server answers.
func (c *Client) Health(ctx context.Context) error {
	return c.get(ctx, "/api/health", nil, nil)
}

// StatusError is an error response that carries no domain error code, such
// as a malformed request.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%d: %s", e.Status, e.Message) }

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("coding_pool server at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// An error body is one small JSON object; read no more than that.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return decodeError(resp.StatusCode, body)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	// Decoded as it streams, with no cap: a list with its relations resolved
	// (agents with their skills' files) runs to tens of megabytes.
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: %w", req.URL.Path, err)
	}
	return nil
}

// decodeError reads the {"error": msg, "code": CODE} contract.
func decodeError(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(body, &e) != nil || e.Error == "" {
		return &StatusError{Status: status, Message: http.StatusText(status)}
	}
	if e.Code != "" {
		return &domain.StructuredError{Code: e.Code, Message: e.Error}
	}
	return &StatusError{Status: status, Message: e.Error}
}
