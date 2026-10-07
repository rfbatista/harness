package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// designSeed reads the JSON the project's design-assets page embeds.
func designSeed(t *testing.T, body string) map[string]any {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script id="design-library-seed" type="application/json">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no design-library-seed in:\n%s", body)
	}
	var seed map[string]any
	if err := json.Unmarshal([]byte(m[1]), &seed); err != nil {
		t.Fatal(err)
	}
	return seed
}

func TestProjectDesignPageSeedsTheProjectAndItsTasks(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/design")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="sessionsDesignLibrary"`, `data-seed="design-library-seed"`,
		`href="/projects/p1"`, `<h1>Design assets</h1>`, `<title>Design assets · coding_pool`,
		`sandbox="allow-scripts"`, "move_artifact_to_project",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	seed := designSeed(t, body)
	if seed["project_id"] != "p1" {
		t.Errorf("project_id = %v", seed["project_id"])
	}
	tasks, _ := seed["tasks"].([]any)
	titles := map[string]string{}
	for _, raw := range tasks {
		tk := raw.(map[string]any)
		titles[tk["id"].(string)] = tk["title"].(string) + " " + tk["href"].(string)
	}
	if len(titles) != 3 || titles["t-feed"] != "Add SSE feed /projects/p1/tasks/t-feed" || titles["t-else"] != "" {
		t.Errorf("seeded tasks = %v, want p1's three with their pages", titles)
	}
}

func TestProjectDesignPageOfAnUnknownProjectIsNotFound(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/nope/design")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "PROJECT_NOT_FOUND") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestShellLinksTheProjectDesignAssets(t *testing.T) {
	for _, path := range []string{"/projects/p1", "/projects/p1/tasks/t-feed", "/projects/p1/design"} {
		if body := get(t, newTestHandler(t, board()), path).Body.String(); !strings.Contains(body, `href="/projects/p1/design"`) {
			t.Errorf("%s does not link the project's design assets", path)
		}
	}
}
