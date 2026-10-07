package web

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestTaskDesignPageSeedsTheTasksAssetsAndTheProjectsTasks(t *testing.T) {
	rec := get(t, newTestHandler(t, designBoard()), "/projects/p1/tasks/t-feed/design")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="sessionsTaskDesign"`, `data-seed="task-design-seed"`,
		`href="/projects/p1/tasks/t-feed"`, `<h1>Design assets</h1>`, `<title>Design assets · Add SSE feed`,
		`role="group"`, `Made in this task`, `Attached from the project`,
		`sandbox="allow-scripts"`, `class="[ palette ]"`, `role="combobox"`, `href="/projects/p1/design"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	seed := seedOf(t, body, "task-design-seed")
	if seed["project_id"] != "p1" || seed["ticket_id"] != "t-feed" {
		t.Errorf("ids = %v %v", seed["project_id"], seed["ticket_id"])
	}
	var ids []string
	for _, raw := range seed["artifacts"].([]any) {
		a := raw.(map[string]any)
		ids = append(ids, a["id"].(string))
		if _, ok := a["attached_ticket_ids"].([]any); !ok {
			t.Errorf("%s has no attached_ticket_ids list: %v", a["id"], a["attached_ticket_ids"])
		}
	}
	slices.Sort(ids)
	if strings.Join(ids, ",") != "a-draft,a-logo" {
		t.Errorf("seeded artifacts = %v, want what t-feed made and what is attached to it", ids)
	}
	tasks := map[string]string{}
	for _, raw := range seed["tasks"].([]any) {
		tk := raw.(map[string]any)
		tasks[tk["id"].(string)] = tk["href"].(string)
	}
	if tasks["t-ship"] != "/projects/p1/tasks/t-ship" || tasks["t-else"] != "" {
		t.Errorf("seeded tasks = %v, want p1's, each with its page", tasks)
	}
}

func TestTaskDesignPageOfAnotherProjectsTaskIsNotFound(t *testing.T) {
	rec := get(t, newTestHandler(t, designBoard()), "/projects/p1/tasks/t-else/design")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "TICKET_NOT_FOUND") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestTaskDesignPageWithoutAnArtifactReaderSeedsNone(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed/design")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := seedOf(t, rec.Body.String(), "task-design-seed")["artifacts"]; got == nil || len(got.([]any)) != 0 {
		t.Errorf("artifacts = %v, want []", got)
	}
}
