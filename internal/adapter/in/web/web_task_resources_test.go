package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// fakeResources reads like planning: a task's resources, oldest first.
type fakeResources []*domain.TicketResource

func (f fakeResources) ListTicketResources(_ context.Context, ticketID string) ([]*domain.TicketResource, error) {
	var out []*domain.TicketResource
	for _, r := range f {
		if r.TicketID == ticketID {
			out = append(out, r)
		}
	}
	return out, nil
}

// fakeComments reads a task's comments, oldest first; since is not used here.
type fakeComments []*domain.TicketComment

func (f fakeComments) ListTicketComments(_ context.Context, ticketID string, _ time.Time) ([]*domain.TicketComment, error) {
	var out []*domain.TicketComment
	for _, c := range f {
		if c.TicketID == ticketID {
			out = append(out, c)
		}
	}
	return out, nil
}

// resourcesBoard is board() with resources and comments on t-feed.
func resourcesBoard() world {
	w := board()
	at := now.Add(-time.Hour)
	w.resources = []*domain.TicketResource{
		{ID: "r-png", TicketID: "t-feed", ProjectID: "p1", Kind: domain.ResourceKind("image"), Title: "wireframe.png", Mime: "image/png", SizeBytes: 412672, ContentURL: "/api/ticket_resources/r-png/content", CreatedAt: at, UpdatedAt: at},
		{ID: "r-link", TicketID: "t-feed", ProjectID: "p1", Kind: domain.ResourceKind("link"), Title: "Figma", URL: "https://figma.com/file/abc", CreatedAt: at, UpdatedAt: at},
		{ID: "r-other", TicketID: "t-docs", ProjectID: "p1", Kind: domain.ResourceKind("file"), Title: "other.pdf", Mime: "application/pdf", SizeBytes: 10, ContentURL: "/api/ticket_resources/r-other/content", CreatedAt: at, UpdatedAt: at},
	}
	w.comments = []*domain.TicketComment{
		{ID: "c1", TicketID: "t-feed", ProjectID: "p1", Author: domain.CommentAuthor("person"), Body: "Keep <the> v2 contract.", CreatedAt: at, UpdatedAt: at},
	}
	return w
}

func TestTaskResourcesPageSeedsTheTasksResourcesAndComments(t *testing.T) {
	rec := get(t, newTestHandler(t, resourcesBoard()), "/projects/p1/tasks/t-feed/resources")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="tasksResources"`, `data-seed="task-resources-seed"`,
		`<title>Resources and comments · Add SSE feed`, `<h1>Resources and comments</h1>`,
		`href="/projects/p1/tasks/t-feed"`, `href="/projects/p1"`,
		// The toolbar must not shrink in the frame: wrapped at 375px it would cover the first section.
		`class="[ toolbar ] [ shrink-0 ]"`,
		`id="resources"`, `id="comments"`, `class="[ resources-page ]" data-scroll`,
		`class="[ lightbox ]"`, `x-data="menu"`, `class="[ composer ]"`,
		`Sessions running on this task get each new comment as a message; edits are not sent.`,
		`<kbd>⌘V</kbd> paste an image`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	seed := seedOf(t, body, "task-resources-seed")
	if seed["project_id"] != "p1" || seed["ticket_id"] != "t-feed" {
		t.Errorf("ids = %v %v", seed["project_id"], seed["ticket_id"])
	}
	resources := seed["resources"].([]any)
	if len(resources) != 2 {
		t.Fatalf("seeded resources = %v, want t-feed's two", resources)
	}
	png := resources[0].(map[string]any)
	if png["id"] != "r-png" || png["kind"] != "image" || png["content_url"] != "/api/ticket_resources/r-png/content" || png["size_bytes"] != float64(412672) {
		t.Errorf("resource in the API's shape = %v", png)
	}
	comments := seed["comments"].([]any)
	if len(comments) != 1 || comments[0].(map[string]any)["body"] != "Keep <the> v2 contract." {
		t.Errorf("seeded comments = %v", comments)
	}
	if strings.Contains(body, "Keep <the>") {
		t.Error("a comment body reached the HTML unescaped")
	}
}

func TestTaskResourcesPageOfAnotherProjectsTaskIsNotFound(t *testing.T) {
	rec := get(t, newTestHandler(t, resourcesBoard()), "/projects/p1/tasks/t-else/resources")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "TICKET_NOT_FOUND") {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
}

func TestTaskResourcesPageWithoutReadersSeedsEmptyLists(t *testing.T) {
	rec := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed/resources")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	seed := seedOf(t, rec.Body.String(), "task-resources-seed")
	if r, ok := seed["resources"].([]any); !ok || len(r) != 0 {
		t.Errorf("resources = %v, want []", seed["resources"])
	}
	if c, ok := seed["comments"].([]any); !ok || len(c) != 0 {
		t.Errorf("comments = %v, want []", seed["comments"])
	}
}

func TestTaskPageLinksItsResourcesAndCommentsWithLiveCounts(t *testing.T) {
	body := get(t, newTestHandler(t, resourcesBoard()), "/projects/p1/tasks/t-feed").Body.String()
	for _, want := range []string{
		`x-data="tasksResourceWatch"`, `data-project-id="p1"`, `data-ticket-id="t-feed"`,
		`data-resource-ids="r-png,r-link"`, `data-comment-ids="c1"`,
		`href="/projects/p1/tasks/t-feed/resources" data-task-resources`,
		`href="/projects/p1/tasks/t-feed/resources#comments" data-task-comments`,
		`x-text="resourceCount">2</span>`, `x-text="commentCount">1</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task page lacks %s", want)
		}
	}
}
