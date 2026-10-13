// Package credentials is the Git credentials page, /git-credentials: the
// commit author identity and every stored credential grouped by host, read
// through ports.GitCredentials (the same shapes the API answers). The
// browser's gitCredentialsPage takes it over live and writes through /api.
// No secret is ever on the page: the port never returns one.
package credentials

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is the Git credentials page.
const Href = "/git-credentials"

// AddHref is the page with its add form open for host (and kind, when set).
func AddHref(host, kind string) string {
	q := url.Values{"host": {host}}
	if kind != "" {
		q.Set("kind", kind)
	}
	return Href + "?" + q.Encode()
}

// Handler serves GET /git-credentials.
type Handler struct {
	Credentials ports.GitCredentials
	Layout      shell.Layout
	Render      shell.Renderer
	Now         func() time.Time
}

// Page renders the page. A failed read still renders it: the browser reads
// the list itself when there is no seed.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	v := View{Now: h.Now(), Host: q.Get("host"), Kind: q.Get("kind"), Credential: q.Get("credential")}
	if v.Kind != string(domain.GitHTTPSToken) && v.Kind != string(domain.GitSSHKey) {
		v.Kind = ""
	}
	list, err := h.Credentials.ListGitCredentials(ctx)
	if err != nil {
		slog.Warn("git credentials page: list not read; the browser reads it", "err", err)
	} else if identity, err := h.Credentials.GetGitIdentity(ctx); err != nil {
		slog.Warn("git credentials page: identity not read; the browser reads it", "err", err)
	} else {
		v.Seed = &Seed{Credentials: list.Credentials, Locked: list.Locked, Identity: identity}
	}
	frame, err := h.Layout(ctx, "Git credentials", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnCredentials = true
	return h.Render(w, r, http.StatusOK, Page(frame, v))
}

// Seed is what gitCredentialsPage starts from: list_git_credentials' answer
// (the credentials and whether the store is locked) with the identity.
type Seed struct {
	Credentials []*domain.GitCredential `json:"credentials"`
	Locked      bool                    `json:"locked"`
	Identity    *domain.GitIdentity     `json:"identity"`
}

// View is what the page renders.
type View struct {
	// Seed is nil when it could not be read.
	Seed *Seed
	Now  time.Time
	// Host and Kind open the add form prefilled (a link from a refusal);
	// Credential marks one row instead.
	Host, Kind, Credential string
}

// Group is one host's credentials.
type Group struct {
	Host string
	Rows []Row
}

// Row is one credential as the page paints it before the browser takes
// over; the browser's view.js credentialRow has the same rules.
type Row struct {
	ID, Name, KindLabel, Meta, PublicKey string
	IsDefault                            bool
}

// Groups are the seed's credentials, one group per host in the list's order.
func (v View) Groups() []Group {
	if v.Seed == nil {
		return nil
	}
	var groups []Group
	for _, c := range v.Seed.Credentials {
		if len(groups) == 0 || groups[len(groups)-1].Host != c.Host {
			groups = append(groups, Group{Host: c.Host})
		}
		g := &groups[len(groups)-1]
		g.Rows = append(g.Rows, NewRow(c, v.Now))
	}
	return groups
}

// Empty reports that the seed has no credentials.
func (v View) Empty() bool { return v.Seed != nil && len(v.Seed.Credentials) == 0 }

// Locked reports a locked store on first paint.
func (v View) Locked() bool { return v.Seed != nil && v.Seed.Locked }

// IdentityUnset reports that no author identity is set.
func (v View) IdentityUnset() bool { return v.Seed != nil && v.Seed.Identity == nil }

// IdentityName and IdentityEmail fill the identity form.
func (v View) IdentityName() string {
	if v.Seed == nil || v.Seed.Identity == nil {
		return ""
	}
	return v.Seed.Identity.Name
}

func (v View) IdentityEmail() string {
	if v.Seed == nil || v.Seed.Identity == nil {
		return ""
	}
	return v.Seed.Identity.Email
}

// NewRow paints c as of now.
func NewRow(c *domain.GitCredential, now time.Time) Row {
	meta := ""
	for _, part := range []string{c.Username, c.Fingerprint, LastUsedLabel(c.LastUsedAt, now)} {
		if part == "" {
			continue
		}
		if meta != "" {
			meta += " · "
		}
		meta += part
	}
	r := Row{ID: c.ID, Name: c.Name, KindLabel: KindLabel(c.Kind), Meta: meta, IsDefault: c.IsDefault}
	if c.PublicKey != nil {
		r.PublicKey = *c.PublicKey
	}
	return r
}

// KindLabel is "HTTPS token" or "SSH key".
func KindLabel(k domain.GitCredentialKind) string {
	if k == domain.GitSSHKey {
		return "SSH key"
	}
	return "HTTPS token"
}

// LastUsedLabel is "last used 3d ago", "last used just now" or "never used".
func LastUsedLabel(at *time.Time, now time.Time) string {
	if at == nil {
		return "never used"
	}
	age := sessions.RelativeTime(*at, now)
	if age == "now" {
		return "last used just now"
	}
	return "last used " + age + " ago"
}

// LockedText is why the store is locked and what to do (the browser's lockedText).
const LockedText = "The server can't read stored secrets: its encryption key is missing or wrong. Nothing was deleted. Set HARNESS_SECRET_KEY, or restore secret.key in the server's data directory, then restart it. Until then you can rename, set defaults, delete and pin, but not add, replace or test."
