// The Models page, /models: the model each session tier starts with. An
// architect-mode session gets the architect model; a session another
// session started gets the implementation model (domain.ResolveSessionModel).
// The server paints each select at the stored model; the browser's modelTier
// (web/src/modules/models) saves a change through /api/update_settings.

package models

import (
	"net/http"
	"slices"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SettingWritingHelper is the writing helper's model setting.
const SettingWritingHelper = "models.assistants.writing_helper"

// Href is the Models page.
const Href = "/models"

// Handler serves GET /models.
type Handler struct {
	Settings ports.SettingsReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Page renders the page.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	frame, err := h.Layout(ctx, "Models", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnModels = true
	v := NewView(h.Settings)
	if !frame.Helper.Available {
		for i := range v.Assistants {
			v.Assistants[i].UnavailableReason, v.Assistants[i].FixURL = frame.Helper.Reason, frame.Helper.FixURL
		}
	}
	return h.Render(w, r, http.StatusOK, Page(frame, v))
}

// View is what the page renders: one row per session tier, and one per
// assistant (a one-off conversation the harness runs, not an agent session).
type View struct {
	Tiers      []Tier
	Assistants []Tier
}

// Tier is one session tier with the model its select shows.
type Tier struct {
	Key, Title, Lead string
	// Current is the stored value, or the built-in model while it is unset.
	Current string
	// NoDefault leaves "default: the CLI chooses" out: the CLI's default may
	// be a slow model, and an assistant wants a fast one.
	NoDefault bool
	// BuiltIn is the model used while the setting is unset; its label reads
	// "(default)".
	BuiltIn string
	// UnavailableReason says why the assistant cannot run, and FixURL where
	// to fix it; empty when it can.
	UnavailableReason, FixURL string
}

// Choice is one model a tier's select offers.
type Choice struct{ Value, Label string }

// Choices are the models a tier takes, in domain.ModelChoices' order.
func (t Tier) Choices() []Choice {
	out := make([]Choice, 0, len(domain.ModelChoices))
	for _, m := range domain.ModelChoices {
		if m == domain.ModelDefault && t.NoDefault {
			continue
		}
		label := m
		if m == domain.ModelDefault {
			label = "default: the CLI chooses"
		}
		if m == t.BuiltIn {
			label += " (default)"
		}
		out = append(out, Choice{Value: m, Label: label})
	}
	if t.NoDefault {
		// An assistant wants a fast model: the quickest first.
		slices.Reverse(out)
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
	}, Assistants: []Tier{
		{
			Key:       SettingWritingHelper,
			Title:     "Writing helper",
			Lead:      "The magic pen in task titles, descriptions and comments. It runs a few short turns per press, so a fast model keeps it responsive. Applies to every project.",
			Current:   current(SettingWritingHelper, "haiku"),
			NoDefault: true,
			BuiltIn:   "haiku",
		},
	}}
}
