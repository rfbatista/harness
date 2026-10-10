package gitcli

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestTheProbeChecksATokenAgainstARemote(t *testing.T) {
	m := plantMachine(t)
	srv := newGitServer(t, "x-access-token", storedToken)
	host := strings.TrimPrefix(srv.URL, "http://")
	host = host[:strings.IndexByte(host, '/')]
	files := authFiles(t, m)
	p := &Probe{Files: files}
	for k, v := range envMap(m.env) {
		t.Setenv(k, v)
	}
	cred := func(token string) ports.GitCredentialSecret {
		return ports.GitCredentialSecret{GitCredential: domain.GitCredential{ID: "c1", Kind: domain.GitHTTPSToken, Host: host, Username: "x-access-token"}, Secret: domain.Secret(token)}
	}
	// The URL's own userinfo is the machine's: dropped.
	withUser := strings.Replace(srv.URL, "http://", "http://machine:machine-secret@", 1)
	res, err := p.Test(context.Background(), cred(storedToken), withUser)
	if err != nil || !res.OK || res.CheckedURL != srv.URL {
		t.Fatalf("Test with the right token = %+v, %v", res, err)
	}
	res, err = p.Test(context.Background(), cred("ghp_wrong"), srv.URL)
	if err != nil || res.OK || res.Message == "" {
		t.Fatalf("Test with a wrong token = %+v, %v; want ok:false with the host's reason", res, err)
	}
	m.untouched(t)
	if ops, _ := filepath.Glob(filepath.Join(files.Root, "ops", "*")); len(ops) != 0 {
		t.Fatalf("operation directories left behind: %v", ops)
	}
}

func TestTheProbeWithoutARepository(t *testing.T) {
	m := plantMachine(t)
	for k, v := range envMap(m.env) {
		t.Setenv(k, v)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	called := ""
	p := &Probe{Files: authFiles(t, m), GitHub: func(_ context.Context, host, token string) (bool, string, string, error) {
		called = host + " " + token
		return false, "Bad credentials", "https://api.github.com/user", nil
	}}
	key := ports.GitCredentialSecret{GitCredential: domain.GitCredential{Kind: domain.GitSSHKey, Host: "127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), Username: "git"}, Secret: "not a key"}
	res, err := p.Test(context.Background(), key, "")
	if err != nil || res.OK || !strings.HasPrefix(res.CheckedURL, "ssh://git@127.0.0.1:") {
		t.Fatalf("an ssh host that hangs up = %+v, %v; want ok:false", res, err)
	}
	m.untouched(t)

	gh := ports.GitCredentialSecret{GitCredential: domain.GitCredential{Kind: domain.GitHTTPSToken, Host: "github.com"}, Secret: storedToken}
	res, err = p.Test(context.Background(), gh, "")
	if err != nil || res.OK || res.Message != "Bad credentials" || called != "github.com "+storedToken {
		t.Fatalf("a GitHub token = %+v, %v (asked %q)", res, err, called)
	}
	other := ports.GitCredentialSecret{GitCredential: domain.GitCredential{Kind: domain.GitHTTPSToken, Host: "gitlab.com"}, Secret: storedToken}
	res, err = p.Test(context.Background(), other, "")
	if err != nil || res.OK || res.Message != "pick a repository to test this token against" {
		t.Fatalf("a token for another host = %+v, %v", res, err)
	}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}
