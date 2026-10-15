package gitcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitCredentialTester = (*Probe)(nil)

// probeTimeout bounds one test of a credential.
const probeTimeout = 20 * time.Second

// GitHubUser asks a GitHub host who token belongs to; ok false with the
// host's reason when it refuses the token.
type GitHubUser func(ctx context.Context, host, token string) (ok bool, message, checkedURL string, err error)

// Probe tests credentials as git and ssh would use them, in a directory of
// its own: nothing of the machine's is used, nothing on the host changes.
type Probe struct {
	Broker *AuthBroker
	// GitHub checks a token on a GitHub host; nil answers ok:false.
	GitHub GitHubUser
}

// Test checks c against remoteURL, or against its host when remoteURL is "".
func (p *Probe) Test(ctx context.Context, c ports.GitCredentialSecret, remoteURL string) (*ports.GitCredentialTest, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	switch {
	case remoteURL != "":
		return p.lsRemote(ctx, c, remoteURL)
	case c.Kind == domain.GitSSHKey:
		return p.sshHost(ctx, c)
	case c.Host == githubHost && p.GitHub != nil:
		ok, msg, checked, err := p.GitHub(ctx, c.Host, c.Secret.Reveal())
		if err != nil {
			return &ports.GitCredentialTest{OK: false, Message: "could not reach " + c.Host + ": " + err.Error(), CheckedURL: checked}, nil
		}
		return &ports.GitCredentialTest{OK: ok, Message: msg, CheckedURL: checked}, nil
	}
	return &ports.GitCredentialTest{OK: false, Message: "pick a repository to test this token against"}, nil
}

const githubHost = "github.com"

// staticSource answers m on every request.
func staticSource(m ports.GitAuthMaterial) ports.GitAuthSource {
	return func() (ports.GitAuthMaterial, error) { return m, nil }
}

func material(c ports.GitCredentialSecret) ports.GitAuthMaterial {
	if c.Kind == domain.GitSSHKey {
		return ports.GitAuthMaterial{SSHKey: c.Secret}
	}
	return ports.GitAuthMaterial{HTTPS: &ports.GitHTTPSLogin{Host: c.Host, Username: c.Username, Token: c.Secret}}
}

// lsRemote lists remoteURL's branches with c alone. A credential in the URL
// itself is dropped: it is the machine's, not the harness's.
func (p *Probe) lsRemote(ctx context.Context, c ports.GitCredentialSecret, remoteURL string) (*ports.GitCredentialTest, error) {
	shown := domain.StripURLUserinfo(remoteURL)
	env, release, err := p.Broker.OpenOperation(staticSource(material(c)))
	if err != nil {
		return nil, err
	}
	defer release()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--", shown)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return &ports.GitCredentialTest{OK: false, Message: refusal(out.String(), err), CheckedURL: shown}, nil
	}
	return &ports.GitCredentialTest{OK: true, Message: "authenticated to " + shown, CheckedURL: shown}, nil
}

// sshHost opens an ssh session to the host as c's user with c's key alone.
// ssh exits 255 for its own failures (refused key, unreachable host); a
// code host that took the key answers with a greeting and another code.
func (p *Probe) sshHost(ctx context.Context, c ports.GitCredentialSecret) (*ports.GitCredentialTest, error) {
	env, release, err := p.Broker.OpenOperation(staticSource(material(c)))
	if err != nil {
		return nil, err
	}
	defer release()
	host, port := c.Host, ""
	if h, pt, ok := strings.Cut(c.Host, ":"); ok {
		host, port = h, pt
	}
	args := []string{"-o", "ConnectTimeout=10", "-T"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, c.Username+"@"+host)
	// GIT_SSH_COMMAND is the exact ssh git would run; eval keeps its quoting.
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", `eval "$GIT_SSH_COMMAND \"\$@\""`, "sh"}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	checked := "ssh://" + c.Username + "@" + c.Host
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() == 255) {
		return &ports.GitCredentialTest{OK: false, Message: refusal(out.String(), err), CheckedURL: checked}, nil
	}
	msg := strings.TrimSpace(out.String())
	if msg == "" {
		msg = "authenticated to " + c.Host
	}
	return &ports.GitCredentialTest{OK: true, Message: msg, CheckedURL: checked}, nil
}

// refusal is the host's reason, from what git or ssh printed.
func refusal(out string, err error) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return err.Error()
	}
	const max = 500
	if len(out) > max {
		out = out[:max] + "…"
	}
	return out
}
