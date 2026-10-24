package models

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
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

func TestTheAssistantsPaneHasTheWritingHelperRowAtHaikuWhileUnset(t *testing.T) {
	v := NewView(settings{})
	if len(v.Assistants) != 1 {
		t.Fatalf("assistants = %+v, want one row", v.Assistants)
	}
	a := v.Assistants[0]
	if a.Key != domain.SettingModelWritingHelper || a.Title != "Writing helper" || a.Current != "haiku" || a.BuiltIn != "haiku" || !a.NoDefault {
		t.Errorf("row = %+v", a)
	}
	if !strings.HasPrefix(a.Lead, "The magic pen in task titles, descriptions and comments.") {
		t.Errorf("lead = %q", a.Lead)
	}
	if a.UnavailableReason != "" || a.FixURL != "" {
		t.Errorf("an unchecked row is available: %+v", a)
	}
}

func TestTheWritingHelperRowShowsTheStoredModel(t *testing.T) {
	v := NewView(settings{domain.SettingModelWritingHelper: "sonnet"})
	if got := selected(t, v.Assistants[0]); got != "sonnet" {
		t.Errorf("selects %q, want sonnet", got)
	}
}

func TestTheWritingHelperRowOffersTheFourModelsAndNeverDefault(t *testing.T) {
	var got []string
	for _, c := range NewView(settings{}).Assistants[0].Choices() {
		got = append(got, c.Value+"|"+c.Label)
	}
	if strings.Join(got, ",") != "haiku|haiku (default),sonnet|sonnet,opus|opus,fable|fable" {
		t.Fatalf("choices = %v", got)
	}
	if n := len(NewView(settings{}).Tiers[0].Choices()); n != len(domain.ModelChoices) {
		t.Errorf("the session tiers still offer default: %d choices", n)
	}
}

func TestAnUnavailableHelperPutsItsReasonAndFixURLOnTheRow(t *testing.T) {
	v := NewView(settings{}).WithWritingHelper(ports.WritingHelperAvailability{Reason: "no agent credential", FixURL: "/agent-isolation#credential"})
	a := v.Assistants[0]
	if a.UnavailableReason != "no agent credential" || a.FixURL != "/agent-isolation#credential" {
		t.Fatalf("row = %+v", a)
	}
	ok := NewView(settings{}).WithWritingHelper(ports.WritingHelperAvailability{Available: true})
	if ok.Assistants[0].UnavailableReason != "" {
		t.Errorf("an available helper has no reason: %+v", ok.Assistants[0])
	}
}
