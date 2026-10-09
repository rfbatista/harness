package web

import (
	"context"
	"errors"
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
		// A session's comment: its agent's name (a link while the session is listed), the Agent mark, no Edit.
		`<strong data-author><a x-bind:href="c.authorHref"`, `data-tone="signal" data-agent-mark>Agent</span>`, `<template x-if="c.canEdit">`,
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
	// The sessions listed on the task, so a session's comment links to its own while it is there.
	if ids := seed["session_ids"].([]any); len(ids) != 2 || ids[0] != "s1" || ids[1] != "s2" {
		t.Errorf("session_ids = %v, want t-feed's s1 and s2", ids)
	}
}

func TestTaskResourcesPageWithoutSessionsSeedsNoSessionIDs(t *testing.T) {
	w := resourcesBoard()
	w.sessions = nil
	seed := seedOf(t, get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed/resources").Body.String(), "task-resources-seed")
	if ids, ok := seed["session_ids"].([]any); !ok || len(ids) != 0 {
		t.Errorf("session_ids = %v, want []", seed["session_ids"])
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

func TestTaskPageWithoutResourceReadersHasNoResourcesLinks(t *testing.T) {
	body := get(t, newTestHandler(t, board()), "/projects/p1/tasks/t-feed").Body.String()
	if strings.Contains(body, "data-task-resources") || strings.Contains(body, "tasksResourceWatch") {
		t.Error("the task page links resources it cannot read")
	}
}

// failingResources fails every read, as a broken store would.
type failingResources struct{}

func (failingResources) ListTicketResources(context.Context, string) ([]*domain.TicketResource, error) {
	return nil, errors.New("disk on fire")
}

func TestTaskPageStillServesWhenTheResourceCountFails(t *testing.T) {
	w := board()
	h := NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, Resources: failingResources{},
		Now: func() time.Time { return now },
	}, builtAssets(t), nil)
	rec := get(t, h, "/projects/p1/tasks/t-feed")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: a toolbar count took the task page down", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "data-task-resources") {
		t.Error("the links show counts that could not be read")
	}
}

func TestTaskResourcesPageSeedsWhoWroteEachComment(t *testing.T) {
	w := resourcesBoard()
	at := now.Add(-time.Minute)
	w.comments = append(w.comments, &domain.TicketComment{ID: "c2", TicketID: "t-feed", ProjectID: "p1", Author: domain.CommentBySession, SessionID: "s1", AgentName: "go-developer", Body: "Done.", CreatedAt: at, UpdatedAt: at})
	seed := seedOf(t, get(t, newTestHandler(t, w), "/projects/p1/tasks/t-feed/resources").Body.String(), "task-resources-seed")
	comments := seed["comments"].([]any)
	if len(comments) != 2 {
		t.Fatalf("seeded comments = %v", comments)
	}
	for i, want := range [][3]string{{"person", "", ""}, {"session", "s1", "go-developer"}} {
		c := comments[i].(map[string]any)
		if c["author"] != want[0] || c["session_id"] != want[1] || c["agent_name"] != want[2] {
			t.Errorf("comment %d = %v, want author/session_id/agent_name %v (always present)", i, c, want)
		}
	}
}
