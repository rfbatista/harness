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

// fakeDesignSystems answers like the design system context: the saved system
// of a project, or the empty record when it has none.
type fakeDesignSystems map[string]*domain.DesignSystem

func (f fakeDesignSystems) GetDesignSystem(_ context.Context, projectID string) (*domain.DesignSystem, error) {
	if ds, ok := f[projectID]; ok {
		return ds, nil
	}
	return &domain.DesignSystem{ProjectID: projectID}, nil
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
	return fakeDesignSystems{"p1": {
		ProjectID: "p1", Exists: true, Version: 3, UpdatedAt: now, UpdatedBy: "person",
		Record: domain.DesignSystemRecord{
			Tokens:     []domain.DesignToken{{Group: "colors", Name: "signal", Type: "color", Value: "oklch(0.5 0.095 182)", Note: "Primary action"}},
			Principles: "# Principles\n",
		},
	}}
}

func TestDesignSystemPageRendersForAProjectWithoutADesignSystem(t *testing.T) {
	w := board()
	w.designs = fakeDesignSystems{}
	rec := get(t, newTestHandler(t, w), "/projects/p1/design-system")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="designSystemPage"`, `data-seed="design-system-seed"`,
		`<h1>Design system</h1>`, `<title>Design system · coding_pool`,
		`role="tablist"`, `data-action="start-blank"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	seed := designSystemSeed(t, body)
	state, _ := seed["state"].(map[string]any)
	if seed["project_id"] != "p1" || seed["base"] != "/projects/p1/design-system" || seed["tab"] != "" ||
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
	seed := designSystemSeed(t, get(t, newTestHandler(t, w), "/projects/p1/design-system").Body.String())
	state, _ := seed["state"].(map[string]any)
	record, _ := state["record"].(map[string]any)
	tokens, _ := record["tokens"].([]any)
	if state["exists"] != true || state["version"] != float64(3) || state["updated_by"] != "person" || len(tokens) != 1 || record["principles"] != "# Principles\n" {
		t.Errorf("state = %v", state)
	}
	if got, _ := state["updated_at"].(string); got != now.Format(time.RFC3339) {
		t.Errorf("updated_at = %q", got)
	}
}

func TestDesignSystemPageWithoutAReaderLeavesTheReadToTheBrowser(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/design-system")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if seed := designSystemSeed(t, rec.Body.String()); seed["state"] != nil {
		t.Errorf("state = %v, want none", seed["state"])
	}
}

func TestDesignSystemPageOpensTheTabOfItsQuery(t *testing.T) {
	h := newTestHandler(t, board())
	for path, want := range map[string]string{
		"/projects/p1/design-system?tab=principles": "principles",
		"/projects/p1/design-system?tab=history":    "history",
		"/projects/p1/design-system?tab=tokens":     "tokens",
		"/projects/p1/design-system?tab=nonsense":   "",
		"/projects/p1/design-system":                "",
	} {
		if got := designSystemSeed(t, get(t, h, path).Body.String())["tab"]; got != want {
			t.Errorf("%s: tab = %v, want %q", path, got, want)
		}
	}
}

func TestDesignSystemPageOfAnUnknownProjectIsNotFound(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/nope/design-system")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "PROJECT_NOT_FOUND") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestProjectNavigationLinksTheDesignSystemAfterDesignAssets(t *testing.T) {
	h := newTestHandler(t, board())
	for _, path := range []string{"/projects/p1", "/projects/p1/design", "/projects/p1/design-system"} {
		body := get(t, h, path).Body.String()
		nav := body[strings.Index(body, `aria-label="Project"`):]
		nav = nav[:strings.Index(nav, "</nav>")]
		assets, system := strings.Index(nav, `<span class="[ label ]">Design assets</span>`), strings.Index(nav, `<span class="[ label ]">Design system</span>`)
		if assets < 0 || system < assets || !strings.Contains(nav[:system], `href="/projects/p1/design-system"`) {
			t.Errorf("%s: Design system does not follow Design assets in %s", path, nav)
		}
	}
	if got := currentProjectPage(get(t, h, "/projects/p1/design-system").Body.String()); got != "/projects/p1/design-system" {
		t.Errorf("current project page = %q", got)
	}
	if got := currentProjectPage(get(t, h, "/projects/p1/design").Body.String()); got != "/projects/p1/design" {
		t.Errorf("Design assets stays the current page of its own route, got %q", got)
	}
}
