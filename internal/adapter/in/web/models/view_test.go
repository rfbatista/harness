package models

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/web/shell"
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

func TestTheClaudeSectionHasOneRowPerSessionModeInTheContractsOrder(t *testing.T) {
	v := NewView(settings{})
	var titles, keys []string
	for _, tier := range v.Tiers {
		titles = append(titles, tier.Title)
		keys = append(keys, tier.Key)
		if tier.Lead == "" {
			t.Errorf("%s has no lead", tier.Title)
		}
	}
	wantTitles := []string{"Default", "Architect", "Implement", "Design", "Product", "Design system"}
	wantKeys := []string{
		"models.claude.mode.default", "models.claude.mode.architect", "models.claude.mode.implement",
		"models.claude.mode.design", "models.claude.mode.product", "models.claude.mode.designsystem",
	}
	if strings.Join(titles, ",") != strings.Join(wantTitles, ",") {
		t.Errorf("titles = %v, want %v", titles, wantTitles)
	}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Errorf("keys = %v, want %v", keys, wantKeys)
	}
}

func TestUnsetTiersShowTheBuiltInModels(t *testing.T) {
	want := map[string]string{
		"Default": "default", "Architect": "opus", "Implement": "sonnet",
		"Design": "default", "Product": "default", "Design system": "default",
	}
	for _, tier := range NewView(settings{}).Tiers {
		if got := selected(t, tier); got != want[tier.Title] {
			t.Errorf("%s selects %q, want %q", tier.Title, got, want[tier.Title])
		}
	}
}

func TestTiersShowTheStoredModels(t *testing.T) {
	v := NewView(settings{
		domain.ModeModelKey(domain.SessionModeArchitect): "fable",
		domain.ModeModelKey(domain.SessionModeImplement): domain.ModelDefault,
		domain.ModeModelKey(domain.SessionModeDesign):    "haiku",
	})
	want := map[string]string{"Architect": "fable", "Implement": domain.ModelDefault, "Design": "haiku", "Default": "default", "Product": "default", "Design system": "default"}
	for _, tier := range v.Tiers {
		if got := selected(t, tier); got != want[tier.Title] {
			t.Errorf("%s selects %q, want %q", tier.Title, got, want[tier.Title])
		}
	}
}

func TestEveryModeRowOffersTheCLIDefaultAndLabelsAPinnedBuiltInModel(t *testing.T) {
	labels := func(title string) map[string]string {
		for _, tier := range NewView(settings{}).Tiers {
			if tier.Title == title {
				out := map[string]string{}
				for _, c := range tier.Choices() {
					out[c.Value] = c.Label
				}
				return out
			}
		}
		t.Fatalf("no %s row", title)
		return nil
	}
	for _, tier := range NewView(settings{}).Tiers {
		if got := labels(tier.Title)["default"]; got != "default: the CLI chooses" {
			t.Errorf("%s labels default %q", tier.Title, got)
		}
	}
	if got := labels("Architect")["opus"]; got != "opus (default)" {
		t.Errorf("Architect labels opus %q", got)
	}
	if got := labels("Implement")["sonnet"]; got != "sonnet (default)" {
		t.Errorf("Implement labels sonnet %q", got)
	}
	if got := labels("Design")["opus"]; got != "opus" {
		t.Errorf("Design labels opus %q", got)
	}
}

func TestThereIsNoImplementationRow(t *testing.T) {
	for _, tier := range NewView(settings{}).Tiers {
		if tier.Title == "Implementation" || tier.Key == "models.claude.implementation" {
			t.Errorf("row %+v is retired", tier)
		}
	}
}

func TestTheAssistantsPaneHasTheWritingHelperRowAtHaikuWhileUnset(t *testing.T) {
	v := NewView(settings{})
	if len(v.Assistants) != 2 {
		t.Fatalf("assistants = %+v, want two rows", v.Assistants)
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
	v := NewView(settings{}).WithWritingHelper(shell.Helper{Reason: "no agent credential", FixURL: "/agent-isolation#credential"})
	a := v.Assistants[0]
	if a.UnavailableReason != "no agent credential" || a.FixURL != "/agent-isolation#credential" {
		t.Fatalf("row = %+v", a)
	}
	ok := NewView(settings{}).WithWritingHelper(shell.Helper{Available: true})
	if ok.Assistants[0].UnavailableReason != "" {
		t.Errorf("an available helper has no reason: %+v", ok.Assistants[0])
	}
}

func TestTheAssistantsPaneHasTheArchitectureAssistantRowAtSonnetWhileUnset(t *testing.T) {
	a := NewView(settings{}).Assistants[1]
	if a.Key != domain.SettingModelArchitectureAssistant || a.Title != "Architecture assistant" || a.Current != "sonnet" || a.BuiltIn != "sonnet" || !a.NoDefault {
		t.Errorf("row = %+v", a)
	}
	if a.UnavailableReason != "" || a.FixURL != "" {
		t.Errorf("an unchecked row is available: %+v", a)
	}
	if got := selected(t, NewView(settings{domain.SettingModelArchitectureAssistant: "opus"}).Assistants[1]); got != "opus" {
		t.Errorf("selects %q, want opus", got)
	}
	var got []string
	for _, c := range a.Choices() {
		got = append(got, c.Value+"|"+c.Label)
	}
	if strings.Join(got, ",") != "haiku|haiku,sonnet|sonnet (default),opus|opus,fable|fable" {
		t.Errorf("choices = %v", got)
	}
}

func TestAnUnavailableArchitectureAssistantPutsItsReasonAndFixURLOnItsRowOnly(t *testing.T) {
	v := NewView(settings{}).WithArchitectureAssistant(shell.Helper{Reason: "no agent credential", FixURL: "/agent-isolation#credential"})
	if a := v.Assistants[1]; a.UnavailableReason != "no agent credential" || a.FixURL != "/agent-isolation#credential" {
		t.Fatalf("row = %+v", a)
	}
	if w := v.Assistants[0]; w.UnavailableReason != "" {
		t.Errorf("the writing helper's row changed: %+v", w)
	}
	if ok := NewView(settings{}).WithArchitectureAssistant(shell.Helper{Available: true}); ok.Assistants[1].UnavailableReason != "" {
		t.Errorf("an available assistant has no reason: %+v", ok.Assistants[1])
	}
}
