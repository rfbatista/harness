package web

import (
	"encoding/json"
	"regexp"
	"testing"

	"operators-mcp/internal/ports"
)

var islandProps = regexp.MustCompile(`(?s)<script[^>]*data-island-props="top-bar"[^>]*>(.*?)</script>`)

// topBarOf is the model a page carries as the top bar island's props. With no
// island renderer the mount is empty, so the props are what the server painted.
func topBarOf(t *testing.T, body string) ports.TopBar {
	t.Helper()
	m := islandProps.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the page carries no top bar island props")
	}
	var bar ports.TopBar
	if err := json.Unmarshal([]byte(m[1]), &bar); err != nil {
		t.Fatalf("the top bar island props are not the top bar model: %v", err)
	}
	return bar
}

// topBarLink is the link with key in the page's top bar.
func topBarLink(t *testing.T, body, key string) (ports.TopBarLink, bool) {
	t.Helper()
	for _, l := range topBarOf(t, body).Links {
		if l.Key == key {
			return l, true
		}
	}
	return ports.TopBarLink{}, false
}
