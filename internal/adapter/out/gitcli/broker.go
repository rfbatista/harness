package gitcli

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"operators-mcp/internal/ports"
)

// requestTimeout bounds one request on a git directory's socket.
const requestTimeout = 10 * time.Second

// servedDir is one git directory's sockets: an ssh-agent and a git
// credential-cache, both answering from src at the moment they are asked,
// so a credential changed in the store is the next request's.
type servedDir struct {
	sockets string
	agentLn net.Listener
	credLn  net.Listener

	mu  sync.Mutex
	src ports.GitAuthSource
}

// serve listens on the sockets in dir (owner-only), replacing any a
// previous run left.
func serve(dir string, src ports.GitAuthSource) (*servedDir, error) {
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return nil, err
		}
	}
	listen := func(name string) (net.Listener, error) {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, fmt.Errorf("git directory socket: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			ln.Close()
			return nil, err
		}
		return ln, nil
	}
	s := &servedDir{sockets: dir, src: src}
	var err error
	if s.agentLn, err = listen(agentSocket); err != nil {
		return nil, err
	}
	if s.credLn, err = listen(credSocket); err != nil {
		s.agentLn.Close()
		return nil, err
	}
	go s.accept(s.agentLn, s.agent)
	go s.accept(s.credLn, s.credential)
	return s, nil
}

func (s *servedDir) setSource(src ports.GitAuthSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.src = src
}

// material reads the directory's credentials from its source now.
func (s *servedDir) material() (ports.GitAuthMaterial, error) {
	s.mu.Lock()
	src := s.src
	s.mu.Unlock()
	return src()
}

func (s *servedDir) close() {
	s.agentLn.Close()
	s.credLn.Close()
}

func (s *servedDir) accept(ln net.Listener, handle func(net.Conn)) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(requestTimeout))
			handle(c)
		}()
	}
}

// agent serves one ssh connection's agent requests with the stored key, as
// it is now. It holds nothing between connections.
func (s *servedDir) agent(c net.Conn) {
	keyring := agent.NewKeyring().(agent.ExtendedAgent)
	m, err := s.material()
	if err != nil {
		slog.Warn("read a git directory's ssh key", "sockets", s.sockets, "err", err)
	} else if key := m.SSHKey.Reveal(); key != "" {
		k, err := ssh.ParseRawPrivateKey([]byte(key))
		if err == nil {
			err = keyring.Add(agent.AddedKey{PrivateKey: k, Comment: "harness"})
		}
		if err != nil {
			slog.Warn("load a git directory's ssh key", "sockets", s.sockets, "err", err)
		}
	}
	_ = agent.ServeAgent(readOnlyAgent{keyring}, c)
}

// readOnlyAgent lists and signs; what would change its keys is refused, so
// a session cannot add one of its own.
type readOnlyAgent struct{ agent.ExtendedAgent }

var errReadOnlyAgent = errors.New("the harness's agent is read-only")

func (readOnlyAgent) Add(agent.AddedKey) error   { return errReadOnlyAgent }
func (readOnlyAgent) Remove(ssh.PublicKey) error { return errReadOnlyAgent }
func (readOnlyAgent) RemoveAll() error           { return errReadOnlyAgent }
func (readOnlyAgent) Lock([]byte) error          { return errReadOnlyAgent }
func (readOnlyAgent) Unlock([]byte) error        { return errReadOnlyAgent }

// credential answers one request of git's credential-cache client (git
// credential-cache --socket): "action=<a>", "timeout=<n>", then the
// credential's lines up to a blank one or the end. Only a get for the stored
// login's host over http(s) is answered; store and erase change nothing.
func (s *servedDir) credential(c net.Conn) {
	var action, protocol, host string
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "action":
			action = v
		case "protocol":
			protocol = v
		case "host":
			host = v
		}
	}
	if action != "get" || (protocol != "https" && protocol != "http") {
		return
	}
	m, err := s.material()
	if err != nil {
		slog.Warn("read a git directory's login", "sockets", s.sockets, "err", err)
		return
	}
	l := m.HTTPS
	if l == nil || l.Token == "" || l.Host != host {
		return
	}
	_, _ = fmt.Fprintf(c, "username=%s\npassword=%s\n", l.Username, l.Token.Reveal())
}
