package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.BranchPublisher = (*BranchPublisher)(nil)

// BranchPublisher publishes a checkout's branches to its origin with the
// credentials of the environment it is given (a GitOperation's), never the
// machine's. It names refs explicitly, so no working tree and no checked-out
// branch is ever touched, and it never forces.
type BranchPublisher struct{}

// NewBranchPublisher returns the adapter.
func NewBranchPublisher() *BranchPublisher { return &BranchPublisher{} }

// OriginURL is remote.origin.url, "" when the checkout has no origin.
func (BranchPublisher) OriginURL(ctx context.Context, repoRoot string) (string, error) {
	cmd := gitCmd(ctx, repoRoot, "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		// git config exits 1 when the key is unset.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("git config: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasRef reports whether ref resolves to a commit.
func (BranchPublisher) HasRef(ctx context.Context, repoRoot, ref string) bool {
	return gitOK(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
}

// FetchBranch updates refs/remotes/origin/<branch> only.
func (BranchPublisher) FetchBranch(ctx context.Context, repoRoot, branch string, env []string) error {
	refspec := "refs/heads/" + branch + ":refs/remotes/origin/" + branch
	_, err := gitNetwork(ctx, repoRoot, env, "fetch", "--quiet", "--no-tags", "origin", refspec)
	if err != nil && strings.Contains(err.Error(), "couldn't find remote ref") {
		return fmt.Errorf("%w: %s", ports.ErrRemoteBranchNotFound, branch)
	}
	return err
}

// Push publishes refs/heads/<branch> to origin under the same name and sets
// its upstream; a rejection carries git's reason.
func (BranchPublisher) Push(ctx context.Context, repoRoot, branch string, env []string) error {
	ref := "refs/heads/" + branch
	_, err := gitNetwork(ctx, repoRoot, env, "push", "--porcelain", "--set-upstream", "origin", ref+":"+ref)
	return err
}

// Subjects lists base..head subjects, newest first.
func (BranchPublisher) Subjects(ctx context.Context, repoRoot, base, head string, limit int) ([]string, error) {
	out, err := gitOutput(ctx, repoRoot, "log", "--format=%s", "-n", strconv.Itoa(limit), "--end-of-options", base+".."+head, "--")
	if err != nil {
		return nil, err
	}
	subjects := []string{}
	for _, s := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if s != "" {
			subjects = append(subjects, s)
		}
	}
	return subjects, nil
}

// gitNetwork runs a git command that talks to a remote, authenticating only
// through env (on top of the neutral local environment, so with no env it
// cannot authenticate at all). It never waits on a prompt nobody can answer.
// The error carries git's own output, whose reason ("! [rejected] … (non-fast-forward)")
// porcelain push writes to stdout; an auth refusal wraps
// ports.ErrRemoteAuthRejected.
func gitNetwork(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = localEnv(env...)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		reason := strings.TrimSpace(strings.TrimSpace(stderr.String()) + "\n" + strings.TrimSpace(stdout.String()))
		if authRefused(reason) {
			return "", fmt.Errorf("git %s: %w: %s", args[0], ports.ErrRemoteAuthRejected, reason)
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, reason)
	}
	return stdout.String(), nil
}

// authRefusals are how git and ssh say the remote refused the credential, or
// that there was none to offer.
var authRefusals = []string{
	"authentication failed", "permission denied", "could not read username", "could not read password",
	"terminal prompts disabled", "the requested url returned error: 401", "the requested url returned error: 403",
	"invalid username or password",
}

func authRefused(reason string) bool {
	r := strings.ToLower(reason)
	for _, s := range authRefusals {
		if strings.Contains(r, s) {
			return true
		}
	}
	return false
}
