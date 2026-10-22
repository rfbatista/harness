package models

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

type settings map[string]string

func (s settings) Setting(key string) string { return s[key] }

// selected is the value of the option the tier's select paints selected.
func selected(t *testing.T, tier Tier) string {
	t.Helper()
	var b bytes.Buffer
	if err := tierRow(tier).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, `data-setting="`+tier.Key+`"`) {
		t.Fatalf("tier %s does not name its setting:\n%s", tier.Title, html)
	}
	i := strings.Index(html, " selected")
	if i < 0 {
		t.Fatalf("tier %s selects nothing:\n%s", tier.Title, html)
	}
	open := strings.LastIndex(html[:i], `value="`) + len(`value="`)
	return html[open : open+strings.Index(html[open:], `"`)]
}

func TestUnsetTiersShowTheBuiltInModels(t *testing.T) {
	v := NewView(settings{})
	if got := selected(t, v.Tiers[0]); got != "opus" {
		t.Errorf("architect selects %q, want opus", got)
	}
	if got := selected(t, v.Tiers[1]); got != "sonnet" {
		t.Errorf("implementation selects %q, want sonnet", got)
	}
}

func TestTiersShowTheStoredModels(t *testing.T) {
	v := NewView(settings{domain.SettingModelArchitect: "fable", domain.SettingModelImplementation: domain.ModelDefault})
	if got := selected(t, v.Tiers[0]); got != "fable" {
		t.Errorf("architect selects %q, want fable", got)
	}
	if got := selected(t, v.Tiers[1]); got != domain.ModelDefault {
		t.Errorf("implementation selects %q, want default", got)
	}
}
