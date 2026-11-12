// The Models page, /models: the model each session mode starts with
// (domain.ModeModels, ADR 0084). The two rows it still shows are the architect
// and implement modes.
// The server paints each select at the stored model; the browser's modelTier
// (web/src/modules/models) saves a change through /api/update_settings.

package models

import (
	"context"
	"net/http"
	"slices"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is the Models page.
const Href = "/models"

// Handler serves GET /models.
type Handler struct {
	Settings ports.SettingsReader
	// Helper says whether the writing helper can run; nil reads as it can.
	Helper func(ctx context.Context) shell.Helper
	// Architecture says whether the architecture assistant can run; nil reads
	// as it can.
	Architecture func(ctx context.Context) shell.Helper
	Layout       shell.Layout
	Render       shell.Renderer
}

// Page renders the page.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	frame, err := h.Layout(ctx, "Models", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnModels = true
	view := NewView(h.Settings)
	if h.Helper != nil {
		view = view.WithWritingHelper(h.Helper(ctx))
	}
	if h.Architecture != nil {
		view = view.WithArchitectureAssistant(h.Architecture(ctx))
	}
	return h.Render(w, r, http.StatusOK, Page(frame, view))
}

// View is what the page renders: one row per session tier, and the
// assistants' rows (assistants are not agent sessions, so they have no tier).
type View struct {
	Tiers      []Tier
	Assistants []Tier
}

// Tier is one session tier with the model its select shows.
type Tier struct {
	Key, Title, Lead string
	// Current is the stored value, or the built-in model while it is unset.
	Current string
	// BuiltIn is the model an unset setting takes, for the "(default)" label.
	BuiltIn string
	// NoDefault leaves "default: the CLI chooses" out of Choices.
	NoDefault bool
	// UnavailableReason and FixURL say why the assistant cannot run and where
	// to fix it; empty while it can.
	UnavailableReason, FixURL string
}

// Choice is one model a tier's select offers.
type Choice struct{ Value, Label string }

// Choices are the models a tier takes. A session tier takes domain.ModelChoices
// in its order. A tier with NoDefault takes the writing helper's fast-first
// list instead, and marks its BuiltIn model "(default)".
func (t Tier) Choices() []Choice {
	models := domain.ModelChoices
	if t.NoDefault {
		models = domain.WritingHelperModelChoices
	}
	out := make([]Choice, 0, len(models))
	for _, m := range models {
		label := m
		switch {
		case m == domain.ModelDefault:
			label = "default: the CLI chooses"
		case m == t.BuiltIn:
			label += " (default)"
		}
		out = append(out, Choice{Value: m, Label: label})
	}
	return out
}

// modeRows names each session mode's row and says which sessions it covers.
var modeRows = map[domain.SessionMode]struct{ Title, Lead string }{
	domain.SessionModeDefault:      {"Default", "Sessions started with no mode: claude runs as the agent defines it."},
	domain.SessionModeArchitect:    {"Architect", "Sessions started in architect mode: they plan the task and hand its parts to other sessions."},
	domain.SessionModeImplement:    {"Implement", "Sessions started in implement mode: they carry out a part of the plan, whoever started them."},
	domain.SessionModeDesign:       {"Design", "Sessions started in design mode: they publish components, images and videos to the Design tab."},
	domain.SessionModeProduct:      {"Product", "Sessions started in product mode, such as feature brainstorms: they interview you and write the PRD."},
	domain.SessionModeDesignSystem: {"Design system", "Sessions started in design-system mode: they work on the project design system."},
}

// NewView reads each session mode's model (domain.ModeModels), one row per
// mode in the order the contract lists them.
func NewView(settings ports.SettingsReader) View {
	var tiers []Tier
	for _, m := range domain.ModeModels(settings.Setting) {
		row := modeRows[m.Mode]
		tiers = append(tiers, Tier{Key: m.Key, Title: row.Title, Lead: row.Lead, Current: m.Current, BuiltIn: m.BuiltIn})
	}
	return View{Tiers: tiers, Assistants: []Tier{{
		Key:       domain.SettingModelWritingHelper,
		Title:     "Writing helper",
		Lead:      "The magic pen in task titles, descriptions and comments. It runs a few short turns per press, so a fast model keeps it responsive. Applies to every project.",
		Current:   domain.WritingHelperModel(settings.Setting(domain.SettingModelWritingHelper)),
		BuiltIn:   domain.DefaultWritingHelperModel,
		NoDefault: true,
	}, {
		Key:       domain.SettingModelArchitectureAssistant,
		Title:     "Architecture assistant",
		Lead:      "The chat on a repository's Architecture page. It reads the code and proposes layers, access and rules, so a stronger model pays off. Applies to every project.",
		Current:   domain.ArchitectureAssistantModel(settings.Setting(domain.SettingModelArchitectureAssistant)),
		BuiltIn:   domain.DefaultArchitectureAssistantModel,
		NoDefault: true,
	}}}
}

// WithWritingHelper says on the writing helper's row why it cannot run, when
// a is not available.
func (v View) WithWritingHelper(a shell.Helper) View {
	assistants := slices.Clone(v.Assistants)
	for i := range assistants {
		if assistants[i].Key == domain.SettingModelWritingHelper && !a.Available {
			assistants[i].UnavailableReason, assistants[i].FixURL = a.Reason, a.FixURL
		}
	}
	v.Assistants = assistants
	return v
}

// WithArchitectureAssistant says on the architecture assistant's row why it
// cannot run, when a is not available.
func (v View) WithArchitectureAssistant(a shell.Helper) View {
	assistants := slices.Clone(v.Assistants)
	for i := range assistants {
		if assistants[i].Key == domain.SettingModelArchitectureAssistant && !a.Available {
			assistants[i].UnavailableReason, assistants[i].FixURL = a.Reason, a.FixURL
		}
	}
	v.Assistants = assistants
	return v
}
