package credentials

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The pages paint what the browser's view.js says: both read
// web/testdata/views/git-credentials.json.
func TestViewsMatchTheBrowser(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/git-credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Credential, Key domain.GitCredential
		Rows            []struct {
			Which          string
			UsedMinutesAgo *int   `json:"used_minutes_ago"`
			KindLabel      string `json:"kind_label"`
			Meta           string
		}
		Remotes  []struct{ URL, Host, Kind string }
		Bindings []struct {
			Pinned    bool
			Resolved  *string
			PullReqs  *string `json:"pull_requests"`
			RemoteURL *string `json:"remote_url"`
			Line      BindingLine
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	pick := func(which string) domain.GitCredential {
		if which == "key" {
			return v.Key
		}
		return v.Credential
	}
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	for _, c := range v.Rows {
		cred := pick(c.Which)
		if c.UsedMinutesAgo != nil {
			at := now.Add(-time.Duration(*c.UsedMinutesAgo) * time.Minute)
			cred.LastUsedAt = &at
		}
		if r := NewRow(&cred, now); r.KindLabel != c.KindLabel || r.Meta != c.Meta {
			t.Errorf("%s row: %q / %q, want %q / %q", c.Which, r.KindLabel, r.Meta, c.KindLabel, c.Meta)
		}
	}
	for _, c := range v.Remotes {
		r, ok := RemoteOf(c.URL)
		if !ok {
			r = Remote{}
		}
		if r.Host != c.Host || string(r.Kind) != c.Kind {
			t.Errorf("RemoteOf(%q) = %+v, want %s %s", c.URL, r, c.Host, c.Kind)
		}
	}
	for _, c := range v.Bindings {
		rc := &domain.RepositoryCredential{RemoteURL: c.RemoteURL}
		if c.Resolved != nil {
			cred := pick(*c.Resolved)
			rc.Resolved = &cred
			if c.Pinned {
				rc.Binding.CredentialID = &cred.ID
			}
		}
		if c.PullReqs != nil {
			cred := pick(*c.PullReqs)
			rc.PullRequests = &cred
		}
		if got := LineOf(rc); got != c.Line {
			t.Errorf("LineOf(%+v) = %+v, want %+v", c, got, c.Line)
		}
	}
}
