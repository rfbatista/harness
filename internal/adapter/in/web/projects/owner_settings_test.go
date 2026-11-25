package projects

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

func renderSettings(t *testing.T, p *domain.Project) string {
	t.Helper()
	var b strings.Builder
	if err := SettingsPage(NewSettingsView(shell.Frame{}, p, nil)).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The Settings page has a "Product owner" section, reachable at #product-owner, whose switch
// the browser draws (a client-rendered island with the project in its props).
func TestTheSettingsPageHasAProductOwnerSection(t *testing.T) {
	got := renderSettings(t, &domain.Project{ID: "p1", Name: "harness"})
	for _, want := range []string{
		`<section id="product-owner"`,
		`<h2 id="settings-product-owner">Product owner</h2>`,
		`<div data-island="owner-settings" data-island-rendered="false"></div>`,
		`<script type="application/json" data-island-props="owner-settings">{"project_id":"p1"}</script>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
}

func TestTheProductOwnerSectionEscapesTheProjectID(t *testing.T) {
	if got := ownerSettingsProps(`</script><b>`); strings.Contains(got, "</script>") || strings.Contains(got, "<b>") {
		t.Errorf("props not escaped: %s", got)
	}
}
