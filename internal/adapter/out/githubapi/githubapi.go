// Package githubapi opens and reads GitHub pull requests over GitHub's REST
// API with a token stored in the harness. The token goes in a request
// header from memory; no process, gh included, ever sees it.
package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.CodeHost = (*Host)(nil)

// Host is ports.CodeHost over GitHub REST.
type Host struct {
	// BaseURL, when set, is the API root for every host (tests). Otherwise
	// github.com is https://api.github.com and another host (GitHub
	// Enterprise) https://<host>/api/v3.
	BaseURL string
	// HTTP is the client; nil is one with a 30 s timeout.
	HTTP *http.Client
}

// New returns the adapter.
func New() *Host { return &Host{} }

func (h *Host) base(host string) string {
	switch {
	case h.BaseURL != "":
		return strings.TrimRight(h.BaseURL, "/")
	case host == "github.com":
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

func (h *Host) client() *http.Client {
	if h.HTTP != nil {
		return h.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// apiError is an answer GitHub gave with an error status.
type apiError struct {
	status int
	reason string
}

func (e *apiError) Error() string {
	return "GitHub answered " + strconv.Itoa(e.status) + ": " + e.reason
}

// call sends one request and decodes a 2xx body into out. A 401 wraps
// ports.ErrCodeHostUnauthorized; any other error status is an *apiError.
func (h *Host) call(ctx context.Context, host string, token domain.Secret, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base(host)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "coding-pool-harness")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token.Reveal())
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return fmt.Errorf("reaching %s: %w", host, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ports.ErrCodeHostUnauthorized, reasonOf(data, resp.Status))
	case resp.StatusCode >= 300:
		return &apiError{status: resp.StatusCode, reason: reasonOf(data, resp.Status)}
	case out == nil:
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("reading GitHub's answer: %w", err)
	}
	return nil
}

func pullsPath(r domain.GitRemote) string {
	return "/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Repo) + "/pulls"
}

// FindOpen is the open pull request whose head is head in r's own owner.
func (h *Host) FindOpen(ctx context.Context, r domain.GitRemote, token domain.Secret, head string) (*ports.HostPullRequest, error) {
	q := url.Values{"head": {r.Owner + ":" + head}, "state": {"open"}, "per_page": {"1"}}
	var prs []restPullRequest
	if err := h.call(ctx, r.Host, token, http.MethodGet, pullsPath(r)+"?"+q.Encode(), nil, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return prs[0].toPort(), nil
}

// Open opens the pull request.
func (h *Host) Open(ctx context.Context, r domain.GitRemote, token domain.Secret, req ports.OpenPullRequest) (*ports.HostPullRequest, error) {
	body := map[string]any{"head": req.Head, "base": req.Base, "title": req.Title, "body": req.Body, "draft": req.Draft}
	var pr restPullRequest
	err := h.call(ctx, r.Host, token, http.MethodPost, pullsPath(r), body, &pr)
	var refused *apiError
	switch {
	case errors.As(err, &refused) && strings.Contains(refused.reason, "already exists"):
		return nil, fmt.Errorf("%w: %s", ports.ErrPullRequestExists, refused.reason)
	case errors.As(err, &refused):
		return nil, domain.PullRequestError(domain.CodePRCreateFailed, "GitHub refused to open the pull request: "+refused.reason)
	case err != nil:
		return nil, err
	}
	return pr.toPort(), nil
}

// Get reads one pull request.
func (h *Host) Get(ctx context.Context, r domain.GitRemote, token domain.Secret, number int) (*ports.HostPullRequest, error) {
	var pr restPullRequest
	if err := h.call(ctx, r.Host, token, http.MethodGet, pullsPath(r)+"/"+strconv.Itoa(number), nil, &pr); err != nil {
		return nil, err
	}
	return pr.toPort(), nil
}

// User asks host who token belongs to: ok with the login, or ok false with
// GitHub's reason when it refuses the token. It changes nothing.
func (h *Host) User(ctx context.Context, host, token string) (bool, string, string, error) {
	checked := h.base(host) + "/user"
	var u struct {
		Login string `json:"login"`
	}
	err := h.call(ctx, host, domain.Secret(token), http.MethodGet, "/user", nil, &u)
	var refused *apiError
	switch {
	case errors.Is(err, ports.ErrCodeHostUnauthorized):
		return false, strings.TrimPrefix(err.Error(), ports.ErrCodeHostUnauthorized.Error()+": "), checked, nil
	case errors.As(err, &refused):
		return false, refused.reason, checked, nil
	case err != nil:
		return false, "", checked, err
	}
	return true, "authenticated to " + host + " as " + u.Login, checked, nil
}

// reasonOf reads GitHub's error body: its message and each validation error.
func reasonOf(body []byte, status string) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil || e.Message == "" {
		if s := strings.TrimSpace(string(body)); s != "" && len(s) < 300 {
			return s
		}
		return status
	}
	parts := []string{e.Message}
	for _, d := range e.Errors {
		switch {
		case d.Message != "":
			parts = append(parts, d.Message)
		case d.Field != "":
			parts = append(parts, d.Field+" "+d.Code)
		}
	}
	return strings.Join(parts, "; ")
}

// restPullRequest is the part of GitHub's REST pull request the harness reads.
type restPullRequest struct {
	Number    int        `json:"number"`
	HTMLURL   string     `json:"html_url"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Draft     bool       `json:"draft"`
	MergedAt  *time.Time `json:"merged_at"`
	CreatedAt time.Time  `json:"created_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p restPullRequest) toPort() *ports.HostPullRequest {
	state := domain.PullRequestOpen
	switch {
	case p.MergedAt != nil:
		state = domain.PullRequestMerged
	case p.State == "closed":
		state = domain.PullRequestClosed
	}
	return &ports.HostPullRequest{
		Number: p.Number, URL: p.HTMLURL, Title: p.Title, Head: p.Head.Ref, Base: p.Base.Ref,
		Draft: p.Draft, State: state, CreatedAt: p.CreatedAt,
	}
}
