// Package ghcli opens and reads GitHub pull requests through the GitHub CLI
// (gh), as the person signed in to it on the server machine. The harness keeps
// no token of its own.
package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.CodeHost = (*Host)(nil)

// hostname is the one host pull requests are opened on for now.
const hostname = "github.com"

// Host is ports.CodeHost over `gh api`.
type Host struct {
	// Bin is the gh executable; "" means "gh" on PATH.
	Bin string
}

// New returns the adapter running gh from PATH.
func New() *Host { return &Host{} }

func (h *Host) bin() string {
	if h.Bin == "" {
		return "gh"
	}
	return h.Bin
}

// Ready checks gh is installed and signed in to github.com. `gh auth token`
// reads the stored sign-in without going over the network.
func (h *Host) Ready(ctx context.Context) error {
	if _, err := exec.LookPath(h.bin()); err != nil {
		return domain.PullRequestError(domain.CodePRToolUnavailable,
			"the GitHub CLI (gh) is not installed on the server machine; install it (https://cli.github.com) and run `gh auth login`")
	}
	if _, _, err := h.run(ctx, nil, "auth", "token", "--hostname", hostname); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.PullRequestError(domain.CodePRToolUnavailable,
			"gh is not signed in to "+hostname+" on the server machine; run `gh auth login`")
	}
	return nil
}

// FindOpen is the open pull request whose head is head in r's own owner.
func (h *Host) FindOpen(ctx context.Context, r domain.GitRemote, head string) (*ports.HostPullRequest, error) {
	out, err := h.api(ctx, nil, "-X", "GET", "repos/"+r.Owner+"/"+r.Repo+"/pulls",
		"-f", "head="+r.Owner+":"+head, "-f", "state=open", "-f", "per_page=1")
	if err != nil {
		return nil, err
	}
	var prs []restPullRequest
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("gh api: reading pull requests: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return prs[0].toPort(), nil
}

// Open opens the pull request.
func (h *Host) Open(ctx context.Context, r domain.GitRemote, req ports.OpenPullRequest) (*ports.HostPullRequest, error) {
	body, _ := json.Marshal(map[string]any{"head": req.Head, "base": req.Base, "title": req.Title, "body": req.Body, "draft": req.Draft})
	out, err := h.api(ctx, body, "-X", "POST", "repos/"+r.Owner+"/"+r.Repo+"/pulls", "--input", "-")
	if err != nil {
		var refused *refusal
		if errors.As(err, &refused) {
			if strings.Contains(refused.reason, "already exists") {
				return nil, fmt.Errorf("%w: %s", ports.ErrPullRequestExists, refused.reason)
			}
			return nil, domain.PullRequestError(domain.CodePRCreateFailed, "GitHub refused to open the pull request: "+refused.reason)
		}
		return nil, err
	}
	var pr restPullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, fmt.Errorf("gh api: reading the opened pull request: %w", err)
	}
	return pr.toPort(), nil
}

// Get reads one pull request.
func (h *Host) Get(ctx context.Context, r domain.GitRemote, number int) (*ports.HostPullRequest, error) {
	out, err := h.api(ctx, nil, "repos/"+r.Owner+"/"+r.Repo+"/pulls/"+strconv.Itoa(number))
	if err != nil {
		return nil, err
	}
	var pr restPullRequest
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, fmt.Errorf("gh api: reading the pull request: %w", err)
	}
	return pr.toPort(), nil
}

// refusal is an answer GitHub gave with an error status; reason is its
// message and validation errors.
type refusal struct{ reason string }

func (r *refusal) Error() string { return r.reason }

// api runs `gh api` against github.com. A refused call is a *refusal, or
// PR_TOOL_UNAVAILABLE when GitHub did not accept the sign-in.
func (h *Host) api(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	out, stderr, err := h.run(ctx, stdin, append([]string{"api", "--hostname", hostname}, args...)...)
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return nil, domain.PullRequestError(domain.CodePRToolUnavailable, "running gh failed: "+err.Error())
	}
	if strings.Contains(stderr, "HTTP 401") || strings.Contains(stderr, "gh auth login") {
		return nil, domain.PullRequestError(domain.CodePRToolUnavailable,
			"GitHub did not accept gh's sign-in on the server machine; run `gh auth login`")
	}
	return nil, &refusal{reason: reasonOf(out, stderr)}
}

// reasonOf reads GitHub's error body: its message and each validation error.
func reasonOf(body []byte, stderr string) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil || e.Message == "" {
		return strings.TrimSpace(stderr)
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

// run executes gh with prompts and update checks off, so it can never wait on
// a person.
func (h *Host) run(ctx context.Context, stdin []byte, args ...string) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, h.bin(), args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), strings.TrimSpace(stderr.String()), err
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
