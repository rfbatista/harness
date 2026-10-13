// Package githubapitest is an in-memory ports.CodeHost, so tests of what
// opens pull requests never reach GitHub.
//
// In a test of the wired server (package app), bind it in place of the
// GitHub adapter and drive it:
//
//	host := githubapitest.New()
//	var prs *workspaces.PullRequests
//	app := fx.New(…the modules…, app.WithCodeHost(host), fx.Populate(&prs))
//
//	host.Reject("ghp_old")               // a call with this token answers ErrCodeHostUnauthorized
//	host.Add(remote, ports.HostPullRequest{Head: "agent/x", Base: "main", Title: "By hand"}) // pre-open: create adopts it
//	host.Opened()                        // what create asked the host to open, in order
//	host.Tokens()                        // the token each call carried, in order
//	host.SetState(remote, 1, domain.PullRequestMerged) // merge or close it on the host
//	prs.RefreshNow(ctx, workspaceID)     // the server asks the host now, past the 30 s throttle,
//	                                     // and returns once the change is on the project feed
//
// remote is the origin as parsed, e.g. domain.GitRemote{Host: "github.com",
// Owner: "acme", Repo: "widgets"}. Calls counts Open, Get and FindOpen.
package githubapitest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.CodeHost = (*FakeHost)(nil)

// FakeHost keeps pull requests per remote in memory. Set the *Err fields to
// script refusals; Gate, when set, holds every Open, Get and FindOpen until it
// is closed or receives, or their context ends.
type FakeHost struct {
	mu       sync.Mutex
	prs      map[domain.GitRemote][]*ports.HostPullRequest
	rejected map[domain.Secret]bool
	tokens   []domain.Secret
	OpenErr  error
	GetErr   error
	FindErr  error
	Gate     chan struct{}
	Now      func() time.Time

	opens, gets, finds int
	opened             []ports.OpenPullRequest
}

// New returns an empty, ready host.
func New() *FakeHost {
	return &FakeHost{prs: map[domain.GitRemote][]*ports.HostPullRequest{}, rejected: map[domain.Secret]bool{}, Now: time.Now}
}

// Reject makes every call carrying token answer ErrCodeHostUnauthorized, as
// GitHub does for a revoked token.
func (h *FakeHost) Reject(token domain.Secret) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rejected[token] = true
}

// Tokens is the token each call carried, in order.
func (h *FakeHost) Tokens() []domain.Secret {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]domain.Secret(nil), h.tokens...)
}

// auth records token and refuses a rejected or empty one; under mu.
func (h *FakeHost) auth(token domain.Secret) error {
	h.tokens = append(h.tokens, token)
	if token == "" || h.rejected[token] {
		return fmt.Errorf("%w: Bad credentials", ports.ErrCodeHostUnauthorized)
	}
	return nil
}

// FindOpen is the newest open pull request for head.
func (h *FakeHost) FindOpen(ctx context.Context, r domain.GitRemote, token domain.Secret, head string) (*ports.HostPullRequest, error) {
	if err := h.wait(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finds++
	if err := h.auth(token); err != nil {
		return nil, err
	}
	if h.FindErr != nil {
		return nil, h.FindErr
	}
	return h.openFor(r, head), nil
}

func (h *FakeHost) openFor(r domain.GitRemote, head string) *ports.HostPullRequest {
	list := h.prs[r]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Head == head && list[i].State == domain.PullRequestOpen {
			cp := *list[i]
			return &cp
		}
	}
	return nil
}

// Open opens a pull request, refusing a second open one for the same head as
// GitHub does.
func (h *FakeHost) Open(ctx context.Context, r domain.GitRemote, token domain.Secret, req ports.OpenPullRequest) (*ports.HostPullRequest, error) {
	if err := h.wait(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opens++
	h.opened = append(h.opened, req)
	if err := h.auth(token); err != nil {
		return nil, err
	}
	if h.OpenErr != nil {
		return nil, h.OpenErr
	}
	if h.openFor(r, req.Head) != nil {
		return nil, fmt.Errorf("%w: A pull request already exists for %s:%s.", ports.ErrPullRequestExists, r.Owner, req.Head)
	}
	return h.add(r, ports.HostPullRequest{Head: req.Head, Base: req.Base, Title: req.Title, Draft: req.Draft, State: domain.PullRequestOpen}), nil
}

func (h *FakeHost) wait(ctx context.Context) error {
	if h.Gate == nil {
		return nil
	}
	select {
	case <-h.Gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Get reads a pull request by number.
func (h *FakeHost) Get(ctx context.Context, r domain.GitRemote, token domain.Secret, number int) (*ports.HostPullRequest, error) {
	if err := h.wait(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gets++
	if err := h.auth(token); err != nil {
		return nil, err
	}
	if h.GetErr != nil {
		return nil, h.GetErr
	}
	for _, p := range h.prs[r] {
		if p.Number == number {
			cp := *p
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("pull request #%d not found", number)
}

// Add puts a pull request on the host as if opened outside the harness;
// Number, URL, State and CreatedAt are filled in when zero.
func (h *FakeHost) Add(r domain.GitRemote, pr ports.HostPullRequest) *ports.HostPullRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.add(r, pr)
}

func (h *FakeHost) add(r domain.GitRemote, pr ports.HostPullRequest) *ports.HostPullRequest {
	if pr.Number == 0 {
		pr.Number = len(h.prs[r]) + 1
	}
	if pr.URL == "" {
		pr.URL = fmt.Sprintf("https://%s/%s/%s/pull/%d", r.Host, r.Owner, r.Repo, pr.Number)
	}
	if pr.State == "" {
		pr.State = domain.PullRequestOpen
	}
	if pr.CreatedAt.IsZero() {
		pr.CreatedAt = h.Now().UTC().Truncate(time.Second)
	}
	stored := pr
	h.prs[r] = append(h.prs[r], &stored)
	cp := stored
	return &cp
}

// SetState moves a pull request on the host, as merging or closing it there
// would.
func (h *FakeHost) SetState(r domain.GitRemote, number int, state domain.PullRequestState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.prs[r] {
		if p.Number == number {
			p.State = state
		}
	}
}

// Calls counts Open, Get and FindOpen calls so far.
func (h *FakeHost) Calls() (opens, gets, finds int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.opens, h.gets, h.finds
}

// Opened is every pull request Open was asked for, in order, refused ones
// included.
func (h *FakeHost) Opened() []ports.OpenPullRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ports.OpenPullRequest(nil), h.opened...)
}
