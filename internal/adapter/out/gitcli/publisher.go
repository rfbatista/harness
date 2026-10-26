package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/ports"
)

var _ ports.BranchPublisher = (*BranchPublisher)(nil)

// BranchPublisher publishes a checkout's branches to its origin with the
// credentials of the environment it is given (a GitOperation's), never the
// machine's. It names refs explicitly, so no working tree and no checked-out
// branch is ever touched, and it never forces. Its reads are bounded by the
// read deadline; push and fetch only by the context they are given, as they
// may rightly take longer.
type BranchPublisher struct{ d config }

// NewBranchPublisher returns the adapter.
func NewBranchPublisher(opts ...Option) *BranchPublisher {
	return &BranchPublisher{d: newConfig(opts)}
}

// OriginURL is remote.origin.url, "" when the checkout has no origin.
func (p BranchPublisher) OriginURL(ctx context.Context, repoRoot string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.d.read())
	defer cancel()
	out, err := p.d.gitCmd(ctx, repoRoot, "config", "--get", "remote.origin.url").Output()
	if err := cut(ctx, "config", err); timedOut(err) {
		return "", err
	}
	if err != nil {
		// git config exits 1 when the key is unset.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("git config: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasRef reports whether ref resolves to a commit. A git that gives no
// answer in time counts as no.
func (p BranchPublisher) HasRef(ctx context.Context, repoRoot, ref string) bool {
	ok, _ := p.d.gitOK(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	return ok
}

// FetchBranch updates refs/remotes/origin/<branch> only.
func (p BranchPublisher) FetchBranch(ctx context.Context, repoRoot, branch string, env []string) error {
	refspec := "refs/heads/" + branch + ":refs/remotes/origin/" + branch
	_, err := p.d.gitNetwork(ctx, repoRoot, env, "fetch", "--quiet", "--no-tags", "origin", refspec)
	if err != nil && strings.Contains(err.Error(), "couldn't find remote ref") {
		return fmt.Errorf("%w: %s", ports.ErrRemoteBranchNotFound, branch)
	}
	return err
}

// Push publishes refs/heads/<branch> to origin under the same name and sets
// its upstream; a rejection carries git's reason.
func (p BranchPublisher) Push(ctx context.Context, repoRoot, branch string, env []string) error {
	ref := "refs/heads/" + branch
	_, err := p.d.gitNetwork(ctx, repoRoot, env, "push", "--porcelain", "--set-upstream", "origin", ref+":"+ref)
	return err
}

// Subjects lists base..head subjects, newest first.
func (p BranchPublisher) Subjects(ctx context.Context, repoRoot, base, head string, limit int) ([]string, error) {
	out, err := p.d.gitOutput(ctx, repoRoot, "log", "--format=%s", "-n", strconv.Itoa(limit), "--end-of-options", base+".."+head, "--")
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
func (d config) gitNetwork(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, d.bin(), args...)
	cmd.Dir = dir
	cmd.Env = localEnv(env...)
	cmd.WaitDelay = waitDelay
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if err := cut(ctx, args[0], err); timedOut(err) {
			return "", err
		}
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

// OriginBranches lists refs/remotes/origin/* in one for-each-ref, and reads
// when origin was last fetched from the age of FETCH_HEAD (any fetch writes
// it). A checkout whose .git is no directory has no known fetch time.
func (p BranchPublisher) OriginBranches(ctx context.Context, repoRoot string) ([]string, time.Time, error) {
	out, err := p.d.gitOutput(ctx, repoRoot, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/")
	if err != nil {
		return nil, time.Time{}, err
	}
	branches := []string{}
	for _, ref := range strings.Split(out, "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/remotes/origin/")
		if ok && name != "" && name != "HEAD" {
			branches = append(branches, name)
		}
	}
	slices.Sort(branches)
	var fetchedAt time.Time
	if fi, err := os.Stat(filepath.Join(repoRoot, ".git", "FETCH_HEAD")); err == nil {
		fetchedAt = fi.ModTime()
	}
	return branches, fetchedAt, nil
}

// FetchOrigin fetches every branch of origin into refs/remotes/origin/*, with
// --prune so a branch deleted there goes. No tags, no working tree.
func (p BranchPublisher) FetchOrigin(ctx context.Context, repoRoot string, env []string) error {
	_, err := p.d.gitNetwork(ctx, repoRoot, env, "fetch", "--quiet", "--no-tags", "--prune", "origin", "+refs/heads/*:refs/remotes/origin/*")
	return err
}
