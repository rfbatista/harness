package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizeGitHost(t *testing.T) {
	for in, want := range map[string]string{
		"github.com":                         "github.com",
		"  GitHub.com ":                      "github.com",
		"https://GitHub.com/acme/widgets":    "github.com",
		"https://github.com:443/acme":        "github.com",
		"https://git.example.com:8443/x":     "git.example.com:8443",
		"git@github.com:acme/widgets.git":    "github.com",
		"ssh://git@gitlab.example.com:22/x":  "gitlab.example.com",
		"ssh://git@gitlab.example.com:2222/": "gitlab.example.com:2222",
		"gitlab.example.com:8443":            "gitlab.example.com:8443",
		"github.com/acme":                    "github.com",
		"":                                   "",
		"   ":                                "",
	} {
		if got := NormalizeGitHost(in); got != want {
			t.Errorf("NormalizeGitHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseGitRemoteURL(t *testing.T) {
	for in, want := range map[string]GitRemoteURL{
		"https://github.com/acme/widgets.git":       {Kind: GitHTTPSToken, Host: "github.com"},
		"https://user:tok@GitHub.com/acme/w.git":    {Kind: GitHTTPSToken, Host: "github.com"},
		"http://git.local:8080/w.git":               {Kind: GitHTTPSToken, Host: "git.local:8080"},
		"git@github.com:acme/widgets.git":           {Kind: GitSSHKey, Host: "github.com"},
		"ssh://git@github.com/acme/widgets.git":     {Kind: GitSSHKey, Host: "github.com"},
		"ssh://git@git.example.com:2222/acme/w.git": {Kind: GitSSHKey, Host: "git.example.com:2222"},
		"git://github.com/acme/widgets.git":         {Kind: "", Host: "github.com"},
	} {
		got, ok := ParseGitRemoteURL(in)
		if !ok || got != want {
			t.Errorf("ParseGitRemoteURL(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "/srv/repos/widgets.git", "file:///srv/repos/w.git", "../w", "C:\\repos\\w", "./a:b"} {
		if got, ok := ParseGitRemoteURL(in); ok {
			t.Errorf("ParseGitRemoteURL(%q) = %+v, want not a remote", in, got)
		}
	}
}

func TestStripURLUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"https://octocat:ghp_x@github.com/acme/w.git": "https://github.com/acme/w.git",
		"https://github.com/acme/w.git":               "https://github.com/acme/w.git",
		"git@github.com:acme/w.git":                   "git@github.com:acme/w.git",
		"https://a:b%zz@github.com/x":                 "https://github.com/x",
	} {
		if got := StripURLUserinfo(in); got != want {
			t.Errorf("StripURLUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveGitCredential(t *testing.T) {
	tokA := &GitCredential{ID: "a", Kind: GitHTTPSToken, Host: "github.com"}
	tokB := &GitCredential{ID: "b", Kind: GitHTTPSToken, Host: "github.com"}
	tokDefault := &GitCredential{ID: "d", Kind: GitHTTPSToken, Host: "github.com", IsDefault: true}
	key := &GitCredential{ID: "k", Kind: GitSSHKey, Host: "github.com"}
	keyB := &GitCredential{ID: "kb", Kind: GitSSHKey, Host: "github.com"}
	keyDefault := &GitCredential{ID: "kd", Kind: GitSSHKey, Host: "github.com", IsDefault: true}
	keyPort := &GitCredential{ID: "kp", Kind: GitSSHKey, Host: "git.example.com:8443"}
	httpsPort := &GitRemoteURL{Kind: GitHTTPSToken, Host: "git.example.com:8443"}
	other := &GitCredential{ID: "o", Kind: GitHTTPSToken, Host: "gitlab.com"}
	https := &GitRemoteURL{Kind: GitHTTPSToken, Host: "github.com"}
	ssh := &GitRemoteURL{Kind: GitSSHKey, Host: "github.com"}
	id := func(c *GitCredential) string {
		if c == nil {
			return ""
		}
		return c.ID
	}
	for _, c := range []struct {
		name   string
		creds  []*GitCredential
		pinned string
		remote *GitRemoteURL
		want   string
	}{
		{"the only token for the host", []*GitCredential{tokA, key, other}, "", https, "a"},
		{"two tokens, no default: none", []*GitCredential{tokA, tokB}, "", https, ""},
		{"the default wins", []*GitCredential{tokA, tokDefault, tokB}, "", https, "d"},
		{"ssh origin takes the key", []*GitCredential{tokA, key}, "", ssh, "k"},
		{"a pin wins", []*GitCredential{tokA, tokDefault}, "a", https, "a"},
		{"a pin to a deleted credential falls back", []*GitCredential{tokA}, "gone", https, "a"},
		{"no origin resolves only a pin", []*GitCredential{tokA}, "", nil, ""},
		{"no origin, pinned", []*GitCredential{tokA}, "a", nil, "a"},
		{"another host resolves nothing", []*GitCredential{other}, "", https, ""},
		{"https origin falls back to the only key", []*GitCredential{key, other}, "", https, "k"},
		{"https origin: a token beats a key", []*GitCredential{key, tokA}, "", https, "a"},
		{"https origin: the default key beats another", []*GitCredential{key, keyDefault}, "", https, "kd"},
		{"https origin: two keys, no default: none", []*GitCredential{key, keyB}, "", https, ""},
		{"https origin with a port takes no key", []*GitCredential{keyPort}, "", httpsPort, ""},
		{"ssh origin never takes a token", []*GitCredential{tokA}, "", ssh, ""},
		{"git:// takes none", []*GitCredential{tokA}, "", &GitRemoteURL{Host: "github.com"}, ""},
	} {
		if got := id(ResolveGitCredential(c.creds, c.pinned, c.remote)); got != c.want {
			t.Errorf("%s: resolved %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := Secret("ghp_topsecret")
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "token", s)
	in := struct {
		Token Secret `json:"token"`
	}{s}
	b, _ := json.Marshal(in)
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s %q", s, in, in, s, s), string(b), buf.String()} {
		if strings.Contains(out, "topsecret") {
			t.Fatalf("the secret printed: %s", out)
		}
	}
	if s.Reveal() != "ghp_topsecret" {
		t.Fatal("Reveal lost the value")
	}
	var back struct {
		Token Secret `json:"token"`
	}
	if err := json.Unmarshal([]byte(`{"token":"abc"}`), &back); err != nil || back.Token.Reveal() != "abc" {
		t.Fatalf("decoding a secret: %v %q", err, back.Token.Reveal())
	}
}

func TestAnSSHKeyServesAnHTTPSOriginOnlyWithoutAPort(t *testing.T) {
	for _, c := range []struct {
		remote GitRemoteURL
		kind   GitCredentialKind
		want   bool
	}{
		{GitRemoteURL{Kind: GitHTTPSToken, Host: "github.com"}, GitHTTPSToken, true},
		{GitRemoteURL{Kind: GitHTTPSToken, Host: "github.com"}, GitSSHKey, true},
		{GitRemoteURL{Kind: GitHTTPSToken, Host: "git.example.com:8443"}, GitSSHKey, false},
		{GitRemoteURL{Kind: GitHTTPSToken, Host: "git.example.com:8443"}, GitHTTPSToken, true},
		{GitRemoteURL{Kind: GitSSHKey, Host: "github.com"}, GitSSHKey, true},
		{GitRemoteURL{Kind: GitSSHKey, Host: "github.com"}, GitHTTPSToken, false},
		{GitRemoteURL{Host: "github.com"}, GitSSHKey, false},
	} {
		if got := c.remote.Takes(c.kind); got != c.want {
			t.Errorf("%+v takes %s = %v, want %v", c.remote, c.kind, got, c.want)
		}
	}
}
