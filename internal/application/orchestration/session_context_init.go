package orchestration

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// initReport is what claude says about itself at the start of a headless
// session: its version and, for each MCP server, whether it connected. A
// terminal session says none of this to the harness, and the harness never
// probes a server to fill the gap (ADR 0099, contract C3).
type initReport struct {
	Version    string `json:"claude_code_version"`
	MCPServers []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"mcp_servers"`
}

// maxStatusReason bounds the error text kept for a failed server, in bytes.
const maxStatusReason = 400

// applyInitReport reads claude's init report of the session's current run and
// sets, on the newest context entry, each reported server's status (connected
// or failed, with claude's error text) and claude's version; a server that
// failed or connected makes a change. A status other than connected or failed
// leaves the server unknown. redact removes the secret values a failed
// server's error might repeat. It reports whether anything changed.
func (s *Service) applyInitReport(ctx context.Context, id string, raw json.RawMessage, redact func(string) string, at time.Time) (bool, error) {
	if s.Contexts == nil {
		return false, nil
	}
	var report initReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return false, nil // not a report this reads: nothing to keep
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	c, err := s.Contexts.Load(ctx, id)
	if err != nil || c == nil || len(c.Entries) == 0 {
		return false, err
	}
	entry := &c.Entries[len(c.Entries)-1]
	changed, kept := false, false
	if report.Version != "" && entry.Facts.ClaudeVersion != report.Version {
		entry.Facts.ClaudeVersion = report.Version
		kept = true
	}
	for _, reported := range report.MCPServers {
		i := slices.IndexFunc(entry.MCPServers, func(sv domain.ContextServer) bool { return sv.Name == reported.Name })
		if i < 0 {
			continue
		}
		server := &entry.MCPServers[i]
		reason := ""
		if reported.Status == domain.MCPFailed {
			reason = statusReason(reported.Error, redact)
		}
		if server.Status == reported.Status && server.StatusReason == reason {
			continue // said before: a repeated report adds no change
		}
		switch reported.Status {
		case domain.MCPConnected:
			server.Status, server.StatusReason = domain.MCPConnected, ""
			c.Changes = append(c.Changes, domain.ContextChange{At: at, Kind: domain.ContextChangeMCPConnected,
				Subject: server.Name, Detail: "claude connected to " + server.Name})
		case domain.MCPFailed:
			server.Status, server.StatusReason = domain.MCPFailed, reason
			detail := "claude could not connect to " + server.Name
			if reason != "" {
				detail += ": " + reason
			}
			c.Changes = append(c.Changes, domain.ContextChange{At: at, Kind: domain.ContextChangeMCPFailed,
				Subject: server.Name, Detail: detail})
		default:
			continue // pending, needs auth, …: said, not guessed
		}
		changed = true
	}
	if !changed && !kept {
		return false, nil
	}
	if err := s.Contexts.Save(ctx, c); err != nil {
		return false, err
	}
	if changed {
		s.contextChanged(id, ports.ContextMCPStatus)
	}
	return changed, nil
}

// statusReason is claude's error text for a failed server, cut to one bounded
// line and with secret values removed.
func statusReason(text string, redact func(string) string) string {
	text = strings.Join(strings.Fields(text), " ")
	if redact != nil {
		text = redact(text)
	}
	text = bearerToken.ReplaceAllString(text, "Bearer [redacted]")
	return domain.CutText(text, maxStatusReason)
}

var bearerToken = regexp.MustCompile(`(?i)\bBearer\s+[^\s"']+`)

const redacted = "[redacted]"

// secretRedactor replaces every secret value of cfg (an environment value, an
// MCP server's environment or header value, and the token of a header like
// "Bearer <token>") in a text. Values under 4 bytes are left: they would
// match everywhere.
func secretRedactor(cfg llmkit.SessionConfig) func(string) string {
	var secrets []string
	add := func(v string) {
		if len(v) >= 4 && !slices.Contains(secrets, v) {
			secrets = append(secrets, v)
		}
	}
	for _, e := range cfg.Env {
		if _, v, ok := strings.Cut(e, "="); ok {
			add(v)
		}
	}
	for _, m := range cfg.MCPServers {
		for _, a := range m.Args {
			add(a)
			if _, v, ok := strings.Cut(a, "="); ok { // --token=value
				add(v)
			}
		}
		addURLSecrets(m.URL, add)
		for _, v := range m.Env {
			add(v)
		}
		for _, v := range m.Headers {
			add(v)
			if f := strings.Fields(v); len(f) > 1 {
				add(f[len(f)-1])
			}
		}
	}
	// Longest first, so a value that holds another is replaced whole.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return func(text string) string {
		for _, v := range secrets {
			text = strings.ReplaceAll(text, v, redacted)
		}
		return text
	}
}

// keepRedactor remembers how to scrub a session's init report: the values of
// its configuration, in memory only, never stored.
func (s *Service) keepRedactor(id string, cfg llmkit.SessionConfig) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if s.redactors == nil {
		s.redactors = map[string]func(string) string{}
	}
	s.redactors[id] = secretRedactor(cfg)
}

// onInit keeps claude's init report of a headless session.
func (s *Service) onInit(id string, ce llmkit.Event) {
	s.contextMu.Lock()
	redact := s.redactors[id]
	s.contextMu.Unlock()
	at := ce.At
	if at.IsZero() {
		at = time.Now()
	}
	if _, err := s.applyInitReport(context.Background(), id, ce.Raw, redact, at); err != nil {
		slog.Warn("keep a session's init report", "session", id, "err", err)
	}
}

// dropRedactor forgets a session's secret values.
func (s *Service) dropRedactor(id string) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	delete(s.redactors, id)
}

// addURLSecrets adds the parts of a server URL that can carry a credential, in
// the forms an error text may repeat: the whole URL, its userinfo and query
// values decoded and as written (percent-escaped), and its long path segments,
// which can hold a key.
func addURLSecrets(raw string, add func(string)) {
	u, err := url.Parse(raw)
	if err != nil {
		return
	}
	add(raw)
	if u.User != nil {
		add(u.User.Username())
		if pw, ok := u.User.Password(); ok {
			add(pw)
		}
		if info, _, ok := strings.Cut(raw[strings.Index(raw, "//")+2:], "@"); ok {
			for _, part := range strings.Split(info, ":") {
				add(part) // as written
			}
		}
	}
	for _, values := range u.Query() {
		for _, v := range values {
			add(v)
		}
	}
	for _, pair := range strings.FieldsFunc(u.RawQuery, func(r rune) bool { return r == '&' || r == ';' }) {
		if _, v, ok := strings.Cut(pair, "="); ok {
			add(v) // as written
		}
	}
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		if len(seg) > 16 {
			add(seg)
		}
	}
}
