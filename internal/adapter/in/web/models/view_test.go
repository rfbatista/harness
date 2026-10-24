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

// options is the select's options as "value=label", in order.
func options(t *testing.T, tier Tier) []string {
	t.Helper()
	var b bytes.Buffer
	if err := tierRow(tier).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	var out []string
	rest := b.String()
	for {
		i := strings.Index(rest, `<option value="`)
		if i < 0 {
			return out
		}
		rest = rest[i+len(`<option value="`):]
		value := rest[:strings.Index(rest, `"`)]
		label := rest[strings.Index(rest, ">")+1:]
		label = label[:strings.Index(label, "<")]
		out = append(out, value+"="+label)
	}
}

func TestTheWritingHelperIsAnAssistantAtHaikuWhileUnset(t *testing.T) {
	v := NewView(settings{})
	if len(v.Assistants) != 1 {
		t.Fatalf("assistants = %d, want the writing helper only", len(v.Assistants))
	}
	a := v.Assistants[0]
	if a.Key != "models.assistants.writing_helper" || a.Title != "Writing helper" {
		t.Errorf("assistant = %+v", a)
	}
	if got := selected(t, a); got != "haiku" {
		t.Errorf("writing helper selects %q, want haiku", got)
	}
	want := []string{"haiku=haiku (default)", "sonnet=sonnet", "opus=opus", "fable=fable"}
	if got := options(t, a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("choices = %v, want %v: no \"default: the CLI chooses\", haiku marked as the default", got, want)
	}
}

func TestTheWritingHelperShowsItsStoredModel(t *testing.T) {
	v := NewView(settings{"models.assistants.writing_helper": "opus"})
	if got := selected(t, v.Assistants[0]); got != "opus" {
		t.Errorf("writing helper selects %q, want opus", got)
	}
}

func TestSessionTiersStillOfferTheCLIDefault(t *testing.T) {
	got := options(t, NewView(settings{}).Tiers[0])
	if got[0] != "default=default: the CLI chooses" {
		t.Errorf("architect choices start with %q", got[0])
	}
}

func TestAnUnavailableHelperSaysWhyAndWhereToFixIt(t *testing.T) {
	a := NewView(settings{}).Assistants[0]
	a.UnavailableReason, a.FixURL = "the harness's Claude is not signed in", "/agent-isolation#credential"
	var b bytes.Buffer
	if err := tierRow(a).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{`data-tone="attention"`, "Pens are disabled: the harness&#39;s Claude is not signed in", `href="/agent-isolation#credential"`} {
		if !strings.Contains(html, want) {
			t.Errorf("row lacks %q:\n%s", want, html)
		}
	}
	var ok bytes.Buffer
	if err := tierRow(NewView(settings{}).Assistants[0]).Render(context.Background(), &ok); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ok.String(), "Pens are disabled") {
		t.Error("an available helper shows the unavailable banner")
	}
}
