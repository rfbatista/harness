package web

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeDesignSystems answers like the design system context: a project's
// design systems by name, each with the saved record or the empty one.
type fakeDesignSystems []*domain.DesignSystem

func (f fakeDesignSystems) ListDesignSystems(_ context.Context, projectID string) ([]*domain.DesignSystemSummary, error) {
	var out []*domain.DesignSystemSummary
	for _, ds := range f {
		if ds.ProjectID == projectID {
			out = append(out, &domain.DesignSystemSummary{
				DesignSystemIdentity: domain.DesignSystemIdentity{ID: ds.ID, ProjectID: ds.ProjectID, Name: ds.Name, Slug: ds.Slug, Description: ds.Description},
				Version:              ds.Version,
			})
		}
	}
	return out, nil
}

func (f fakeDesignSystems) GetDesignSystem(_ context.Context, id string) (*domain.DesignSystem, error) {
	for _, ds := range f {
		if ds.ID == id {
			c := *ds
			c.Record.Normalize()
			return &c, nil
		}
	}
	return nil, &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "no such design system"}
}

// designSystemSeed reads the JSON the design system page embeds.
func designSystemSeed(t *testing.T, body string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script id="design-system-seed" type="application/json">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no design-system-seed in:\n%s", body)
	}
	var seed map[string]any
	if err := json.Unmarshal([]byte(m[1]), &seed); err != nil {
		t.Fatal(err)
	}
	return seed
}

func savedDesignSystem() fakeDesignSystems {
	return fakeDesignSystems{{
		ID: "ds-app", ProjectID: "p1", Name: "App", Slug: "app", Description: "Marketing and product apps",
		Exists: true, Version: 3, UpdatedAt: now, UpdatedBy: "person",
		Record: domain.DesignSystemRecord{
			Tokens:     []domain.DesignToken{{Group: "colors", Name: "signal", Type: "color", Value: "oklch(0.5 0.095 182)", Note: "Primary action"}},
			Principles: "# Principles\n",
		},
	}}
}

func blankDesignSystem() fakeDesignSystems {
	return fakeDesignSystems{{ID: "ds-brand", ProjectID: "p1", Name: "Brand", Slug: "brand", Description: "The brand site"}}
}

const appPage = "/projects/p1/design-systems/app"

func TestDesignSystemPageRendersForADesignSystemAtVersionZero(t *testing.T) {
	w := board()
	w.designs = blankDesignSystem()
	rec := get(t, newTestHandler(t, w), "/projects/p1/design-systems/brand")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="designSystemPage"`, `data-seed="design-system-seed"`,
		`data-design-system-name x-text="name">Brand<`, `data-design-system-slug x-text="slug">brand<`, `The brand site`, `<title>Brand · Design systems · coding_pool`,
		`role="tablist"`, `data-action="start-blank"`, `data-action="import" disabled`, `data-action="spawn" disabled`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	seed := designSystemSeed(t, body)
	state, _ := seed["state"].(map[string]any)
	if seed["project_id"] != "p1" || seed["design_system_id"] != "ds-brand" || seed["name"] != "Brand" || seed["slug"] != "brand" ||
		seed["description"] != "The brand site" || seed["base"] != "/projects/p1/design-systems/brand" || seed["tab"] != "" ||
		state["exists"] != false || state["version"] != float64(0) {
		t.Errorf("seed = %v", seed)
	}
	if tokens, _ := state["record"].(map[string]any)["tokens"].([]any); tokens == nil || len(tokens) != 0 {
		t.Errorf("an empty record seeds [] tokens, got %v", state["record"])
	}
}

func TestDesignSystemPageSeedsTheSavedVersion(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	seed := designSystemSeed(t, get(t, newTestHandler(t, w), appPage).Body.String())
	state, _ := seed["state"].(map[string]any)
	record, _ := state["record"].(map[string]any)
	tokens, _ := record["tokens"].([]any)
	if seed["design_system_id"] != "ds-app" || state["exists"] != true || state["version"] != float64(3) || state["updated_by"] != "person" || len(tokens) != 1 || record["principles"] != "# Principles\n" {
		t.Errorf("seed = %v", seed)
	}
	if got, _ := state["updated_at"].(string); got != now.Format(time.RFC3339) {
		t.Errorf("updated_at = %q", got)
	}
}

func TestDesignSystemPageOpensTheTabOfItsQuery(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	h := newTestHandler(t, w)
	for path, want := range map[string]string{
		appPage + "?tab=principles":   "principles",
		appPage + "?tab=history":      "history",
		appPage + "?tab=tokens":       "tokens",
		appPage + "?tab=repositories": "repositories",
		appPage + "?tab=nonsense":     "",
		appPage:                       "",
	} {
		if got := designSystemSeed(t, get(t, h, path).Body.String())["tab"]; got != want {
			t.Errorf("%s: tab = %v, want %q", path, got, want)
		}
	}
}

func TestDesignSystemPageOfAnUnknownSlugOrProjectIsNotFound(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	h := newTestHandler(t, w)
	for path, code := range map[string]string{
		"/projects/p1/design-systems/nope":  "DESIGN_SYSTEM_NOT_FOUND",
		"/projects/p2/design-systems/app":   "DESIGN_SYSTEM_NOT_FOUND",
		"/projects/nope/design-systems/app": "PROJECT_NOT_FOUND",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: status %d, want 404 %s:\n%s", path, rec.Code, code, rec.Body.String())
		}
	}
}

func TestTheOldDesignSystemRouteIsGone(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	// What the web client does not own falls through to the legacy designer.
	if rec := get(t, newTestHandler(t, w), "/projects/p1/design-system"); strings.Contains(rec.Body.String(), "designSystemPage") || !strings.Contains(rec.Body.String(), "legacy designer") {
		t.Errorf("the old route still serves the page (status %d)", rec.Code)
	}
}

func TestDesignSystemsRouteGoesToTheFirstDesignSystem(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	rec := get(t, newTestHandler(t, w), "/projects/p1/design-systems")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != appPage {
		t.Errorf("status %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDesignSystemsRouteOfAProjectWithNoneSaysSo(t *testing.T) {
	w := board()
	w.designs = fakeDesignSystems{}
	rec := get(t, newTestHandler(t, w), "/projects/p1/design-systems")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No design systems yet") {
		t.Errorf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec := get(t, newTestHandler(t, board()), "/projects/nope/design-systems"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown project: status %d", rec.Code)
	}
}

func TestProjectNavigationLinksTheDesignSystemsAfterDesignAssets(t *testing.T) {
	w := board()
	w.designs = savedDesignSystem()
	h := newTestHandler(t, w)
	for _, path := range []string{"/projects/p1", "/projects/p1/design", appPage} {
		body := get(t, h, path).Body.String()
		nav := body[strings.Index(body, `aria-label="Project"`):]
		nav = nav[:strings.Index(nav, "</nav>")]
		assets, system := strings.Index(nav, `<span class="[ label ]">Design assets</span>`), strings.Index(nav, `<span class="[ label ]">Design systems</span>`)
		if assets < 0 || system < assets || !strings.Contains(nav[:system], `href="/projects/p1/design-systems"`) {
			t.Errorf("%s: Design systems does not follow Design assets in %s", path, nav)
		}
	}
	if got := currentProjectPage(get(t, h, appPage).Body.String()); got != "/projects/p1/design-systems" {
		t.Errorf("current project page = %q", got)
	}
	if got := currentProjectPage(get(t, h, "/projects/p1/design").Body.String()); got != "/projects/p1/design" {
		t.Errorf("Design assets stays the current page of its own route, got %q", got)
	}
}
