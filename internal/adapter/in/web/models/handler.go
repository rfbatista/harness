// The Models page, /models: the model each session tier starts with. An
// architect-mode session gets the architect model; a session another
// session started gets the implementation model (domain.ResolveSessionModel).
// The server paints each select at the stored model; the browser's modelTier
// (web/src/modules/models) saves a change through /api/update_settings.

package models

import (
	"net/http"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

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
	return h.Render(w, r, http.StatusOK, Page(frame, NewView(h.Settings)))
}

// View is what the page renders: one row per tier.
type View struct {
	Tiers []Tier
}

// Tier is one session tier with the model its select shows.
type Tier struct {
	Key, Title, Lead string
	// Current is the stored value, or the built-in model while it is unset.
	Current string
}

// Choice is one model a tier's select offers.
type Choice struct{ Value, Label string }

// Choices are the models a tier takes, in domain.ModelChoices' order.
func (Tier) Choices() []Choice {
	out := make([]Choice, 0, len(domain.ModelChoices))
	for _, m := range domain.ModelChoices {
		label := m
		if m == domain.ModelDefault {
			label = "default: the CLI chooses"
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
	}}
}
