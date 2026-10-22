package credentials

import (
	"regexp"
	"strings"

	"operators-mcp/internal/domain"
)

// The rules below mirror web/src/modules/credentials (domain/credential.js
// remoteOf, presentation/view.js bindingLine); web/testdata/views/
// git-credentials.json pins both sides.

var (
	schemeRe = regexp.MustCompile(`^(?i)([a-z][a-z0-9+.-]*)://`)
	scpRe    = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):`)
)

// Remote is what an origin needs: its host and the kind of credential its
// scheme takes.
type Remote struct {
	Host string
	Kind domain.GitCredentialKind
}

// RemoteOf reads an origin URL; ok is false for a local or unreadable one.
func RemoteOf(url string) (Remote, bool) {
	u := strings.TrimSpace(url)
	if u == "" {
		return Remote{}, false
	}
	if m := schemeRe.FindStringSubmatch(u); m != nil {
		var kind domain.GitCredentialKind
		switch strings.ToLower(m[1]) {
		case "https", "http":
			kind = domain.GitHTTPSToken
		case "ssh", "git+ssh":
			kind = domain.GitSSHKey
		default:
			return Remote{}, false
		}
		host := normalizeHost(u)
		return Remote{Host: host, Kind: kind}, host != ""
	}
	if m := scpRe.FindStringSubmatch(u); m != nil && !strings.HasPrefix(u[len(m[0]):], "//") {
		return Remote{Host: strings.ToLower(m[1]), Kind: domain.GitSSHKey}, true
	}
	return Remote{}, false
}

func normalizeHost(s string) string {
	h := strings.ToLower(strings.TrimSpace(s))
	h = schemeRe.ReplaceAllString(h, "")
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if i := strings.Index(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	return h
}

// BindingLine is what a repository's pushes use and what its pull requests
// use, each in words, and where to add a credential when nothing fits.
type BindingLine struct {
	State   string `json:"state"` // pinned, automatic, none, local
	Name    string `json:"name"`
	Text    string `json:"text"`
	AddHref string `json:"add_href"`
	// PullText says what pull requests use; "" when the repository has no
	// remote to open one on. GitHub's API takes only a token, never a key.
	PullText    string `json:"pull_text"`
	PullAddHref string `json:"pull_add_href"`
}

// LineOf reads a repository's credential as its row says it.
func LineOf(rc *domain.RepositoryCredential) BindingLine {
	remote := ""
	if rc.RemoteURL != nil {
		remote = *rc.RemoteURL
	}
	r, readable := RemoteOf(remote)
	line := pushLine(rc, remote, r, readable)
	if remote != "" && readable {
		line.PullText, line.PullAddHref = pullLine(rc, r)
	}
	return line
}

// viaSSH is true when rc's credential is an SSH key serving an https origin.
func viaSSH(rc *domain.RepositoryCredential, r Remote, readable bool) bool {
	return readable && r.Kind == domain.GitHTTPSToken && rc.Resolved != nil && rc.Resolved.Kind == domain.GitSSHKey
}

func pushLine(rc *domain.RepositoryCredential, remote string, r Remote, readable bool) BindingLine {
	if rc.Binding.CredentialID != nil && rc.Resolved != nil {
		text := "pinned"
		if viaSSH(rc, r, readable) {
			text += " (SSH fallback)"
		}
		return BindingLine{State: "pinned", Name: rc.Resolved.Name, Text: text}
	}
	if remote == "" {
		return BindingLine{State: "local", Text: "Local only: no remote, no credential needed."}
	}
	if c := rc.Resolved; c != nil {
		why := "the only " + KindLabel(c.Kind) + " for " + c.Host
		if c.IsDefault {
			why = "default for " + c.Host
		}
		if viaSSH(rc, r, readable) {
			if c.IsDefault {
				why = "default " + KindLabel(c.Kind) + " for " + c.Host
			}
			why = "SSH fallback: " + why
		}
		return BindingLine{State: "automatic", Name: c.Name, Text: "automatic (" + why + ")"}
	}
	if !readable {
		return BindingLine{State: "none", Text: "The origin is not an https or ssh URL, so no credential applies."}
	}
	what := "token"
	if r.Kind == domain.GitSSHKey {
		what = "SSH key"
	} else if !strings.Contains(r.Host, ":") {
		what = "token or SSH key"
	}
	return BindingLine{State: "none", Text: "No " + what + " for " + r.Host + ": pushes from this repository will fail.", AddHref: AddHref(r.Host, string(r.Kind))}
}

// pullLine says what pull requests on r use: the token resolved for them, or
// that there is none and where to add one.
func pullLine(rc *domain.RepositoryCredential, r Remote) (text, addHref string) {
	c := rc.PullRequests
	if c == nil {
		return "Pull requests: no token for " + r.Host + ", so they will fail.", AddHref(r.Host, string(domain.GitHTTPSToken))
	}
	why := "the only " + KindLabel(c.Kind) + " for " + c.Host
	switch {
	case rc.Binding.CredentialID != nil && rc.Resolved != nil && rc.Resolved.ID == c.ID:
		why = "pinned"
	case c.IsDefault:
		why = "default for " + c.Host
	}
	return "Pull requests: " + c.Name + " (" + why + ")", ""
}

// RepositoriesSeed is what the Repositories page's repositoryCredential rows
// start from: the credentials (list_git_credentials' answer) and every
// repository's binding (list_repository_credentials').
type RepositoriesSeed struct {
	ProjectID    string                         `json:"project_id"`
	Credentials  []*domain.GitCredential        `json:"credentials"`
	Locked       bool                           `json:"locked"`
	Repositories []*domain.RepositoryCredential `json:"repositories"`
}

// Lines are the seed's repositories as their rows say them, by repository id.
func (s *RepositoriesSeed) Lines() map[string]BindingLine {
	lines := map[string]BindingLine{}
	if s == nil {
		return lines
	}
	for _, rc := range s.Repositories {
		lines[rc.Binding.RepositoryID] = LineOf(rc)
	}
	return lines
}
