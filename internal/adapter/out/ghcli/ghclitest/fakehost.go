// Package ghclitest is an in-memory ports.CodeHost, so tests of what opens
// pull requests never reach GitHub.
package ghclitest

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
// script refusals; Gate, when set, holds every Open until it is closed or
// receives.
type FakeHost struct {
	mu       sync.Mutex
	prs      map[domain.GitRemote][]*ports.HostPullRequest
	ReadyErr error
	OpenErr  error
	GetErr   error
	FindErr  error
	Gate     chan struct{}
	Now      func() time.Time

	opens, gets, finds int
}

// New returns an empty, ready host.
func New() *FakeHost {
	return &FakeHost{prs: map[domain.GitRemote][]*ports.HostPullRequest{}, Now: time.Now}
}

// Ready answers ReadyErr.
func (h *FakeHost) Ready(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ReadyErr
}

// FindOpen is the newest open pull request for head.
func (h *FakeHost) FindOpen(_ context.Context, r domain.GitRemote, head string) (*ports.HostPullRequest, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finds++
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
func (h *FakeHost) Open(ctx context.Context, r domain.GitRemote, req ports.OpenPullRequest) (*ports.HostPullRequest, error) {
	if h.Gate != nil {
		select {
		case <-h.Gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opens++
	if h.OpenErr != nil {
		return nil, h.OpenErr
	}
	if h.openFor(r, req.Head) != nil {
		return nil, fmt.Errorf("%w: A pull request already exists for %s:%s.", ports.ErrPullRequestExists, r.Owner, req.Head)
	}
	return h.add(r, ports.HostPullRequest{Head: req.Head, Base: req.Base, Title: req.Title, Draft: req.Draft, State: domain.PullRequestOpen}), nil
}

// Get reads a pull request by number.
func (h *FakeHost) Get(_ context.Context, r domain.GitRemote, number int) (*ports.HostPullRequest, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gets++
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
