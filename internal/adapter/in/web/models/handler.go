// The Models page, /models: the model each session tier starts with. An
// architect-mode session gets the architect model; a session another
// session started gets the implementation model (domain.ResolveSessionModel).
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
	Helper func(ctx context.Context) ports.WritingHelperAvailability
	Layout shell.Layout
	Render shell.Renderer
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
	// BuiltIn is the model an unset setting takes, for the "(default)" label;
	// "" for the session tiers.
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

// NewView reads each tier's stored model.
func NewView(settings ports.SettingsReader) View {
	current := func(key, builtIn string) string {
		if v := settings.Setting(key); v != "" {
			return v
		}
		return builtIn
	}
	return View{Tiers: []Tier{
		{
			Key:     domain.SettingModelArchitect,
			Title:   "Architect",
			Lead:    "Sessions started in architect mode: they plan the task and hand its parts to other sessions.",
			Current: current(domain.SettingModelArchitect, domain.DefaultArchitectModel),
		},
		{
			Key:     domain.SettingModelImplementation,
			Title:   "Implementation",
			Lead:    "Sessions another session started, through start_task_session: they carry out a part of the plan.",
			Current: current(domain.SettingModelImplementation, domain.DefaultImplementationModel),
		},
	}, Assistants: []Tier{{
		Key:       domain.SettingModelWritingHelper,
		Title:     "Writing helper",
		Lead:      "The magic pen in task titles, descriptions and comments. It runs a few short turns per press, so a fast model keeps it responsive. Applies to every project.",
		Current:   domain.WritingHelperModel(settings.Setting(domain.SettingModelWritingHelper)),
		BuiltIn:   domain.DefaultWritingHelperModel,
		NoDefault: true,
	}}}
}

// WithWritingHelper says on the writing helper's row why it cannot run, when
// a is not available.
func (v View) WithWritingHelper(a ports.WritingHelperAvailability) View {
	assistants := slices.Clone(v.Assistants)
	for i := range assistants {
		if assistants[i].Key == domain.SettingModelWritingHelper && !a.Available {
			assistants[i].UnavailableReason, assistants[i].FixURL = a.Reason, a.FixURL
		}
	}
	v.Assistants = assistants
	return v
}
