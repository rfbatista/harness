// Package portstest holds contract suites for the driving ports that cross
// the network. The same suite runs against the application service that
// implements a port and, once tui-client's HTTP adapters exist, against those
// adapters talking to the real router: if the two disagree, the wire is wrong.
//
// The suites pin what a caller can rely on without knowing which side it
// holds — round trips and the error codes it branches on — not validation
// details each service already tests.
package portstest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// wantCode fails t unless err carries code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := errs.Code(err); got != code {
		t.Fatalf("error code = %q (err %v), want %q", got, err, code)
	}
}

// mustTicket returns a helper that fails the test on err and returns tk
// otherwise, so a board call can be used as a value: must(b.GetTicket(...)).
func mustTicket(t *testing.T) func(tk *domain.Ticket, err error) *domain.Ticket {
	return func(tk *domain.Ticket, err error) *domain.Ticket {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
}

func ticketIDs(list []*domain.Ticket) []string {
	ids := make([]string, 0, len(list))
	for _, tk := range list {
		ids = append(ids, tk.ID)
	}
	return ids
}

// ProjectsConformance runs the projects contract against a fresh, empty
// ports.Projects from newPort.
func ProjectsConformance(t *testing.T, newPort func(t *testing.T) ports.Projects) {
	ctx := context.Background()

	t.Run("project round trip", func(t *testing.T) {
		p := newPort(t)
		created, err := p.CreateProject(ctx, "proj", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.GetProject(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != created.ID || got.Name != "proj" {
			t.Fatalf("GetProject = %+v, want %+v", got, created)
		}
		list, err := p.ListProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != created.ID {
			t.Fatalf("ListProjects = %d projects, want only %s", len(list), created.ID)
		}
	})

	t.Run("missing project is PROJECT_NOT_FOUND", func(t *testing.T) {
		p := newPort(t)
		_, err := p.GetProject(ctx, "missing")
		wantCode(t, err, "PROJECT_NOT_FOUND")
		wantCode(t, p.DeleteProject(ctx, "missing"), "PROJECT_NOT_FOUND")
		_, err = p.CreateRepository(ctx, "missing", "api", "", "https://example.com/api.git", t.TempDir())
		wantCode(t, err, "PROJECT_NOT_FOUND")
	})

	t.Run("deleted project is gone", func(t *testing.T) {
		p := newPort(t)
		created, err := p.CreateProject(ctx, "proj", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := p.DeleteProject(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
		_, err = p.GetProject(ctx, created.ID)
		wantCode(t, err, "PROJECT_NOT_FOUND")
	})

	t.Run("repository round trip", func(t *testing.T) {
		p := newPort(t)
		proj, err := p.CreateProject(ctx, "proj", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		repo, err := p.CreateRepository(ctx, proj.ID, "api", "the api", "https://example.com/api.git", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.GetRepository(ctx, repo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ProjectID != proj.ID || got.Name != "api" {
			t.Fatalf("GetRepository = %+v, want api in %s", got, proj.ID)
		}
		list, err := p.ListRepositories(ctx, proj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != repo.ID {
			t.Fatalf("ListRepositories = %d repositories, want only %s", len(list), repo.ID)
		}
	})

	t.Run("missing repository is REPOSITORY_NOT_FOUND", func(t *testing.T) {
		p := newPort(t)
		_, err := p.GetRepository(ctx, "missing")
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
	})

	t.Run("create and update refuse a bad name or root", func(t *testing.T) {
		p := newPort(t)
		taken, err := p.CreateProject(ctx, "Taken", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.CreateProject(ctx, " ", t.TempDir())
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.CreateProject(ctx, "taken", t.TempDir())
		wantCode(t, err, "PROJECT_NAME_TAKEN")
		_, err = p.CreateProject(ctx, "other", "relative/dir")
		wantCode(t, err, "PROJECT_ROOT_INVALID")

		other, err := p.CreateProject(ctx, "Other", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.UpdateProject(ctx, other.ID, "TAKEN", "")
		wantCode(t, err, "PROJECT_NAME_TAKEN")
		_, err = p.UpdateProject(ctx, other.ID, "", "/no/such/dir/anywhere")
		wantCode(t, err, "PROJECT_ROOT_INVALID")
		_, err = p.UpdateProject(ctx, "", "x", "")
		wantCode(t, err, "INVALID_INPUT")

		got, err := p.UpdateProject(ctx, taken.ID, "Renamed", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Renamed" || got.RootDir != taken.RootDir {
			t.Fatalf("UpdateProject = %+v, want Renamed keeping root %q", got, taken.RootDir)
		}
	})

	t.Run("summaries", func(t *testing.T) {
		p := newPort(t)
		list, err := p.ListProjectSummaries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if list == nil || len(list) != 0 {
			t.Fatalf("ListProjectSummaries on no projects = %#v, want empty, not nil", list)
		}
		b, err := p.CreateProject(ctx, "beta", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		a, err := p.CreateProject(ctx, "Alpha", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.CreateRepository(ctx, b.ID, "api", "", "https://example.com/api.git", t.TempDir()); err != nil {
			t.Fatal(err)
		}
		list, err = p.ListProjectSummaries(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Project.ID != a.ID || list[1].Project.ID != b.ID {
			t.Fatalf("ListProjectSummaries = %+v, want Alpha then beta", list)
		}
		if got := list[1]; got.Project.Name != "beta" || got.Project.RootDir != b.RootDir || got.RepositoryCount != 1 ||
			got.OpenTaskCount != 0 || got.RunningSessionCount != 0 || got.LastActivityAt != nil {
			t.Fatalf("beta summary = %+v", got)
		}
	})
}

// TicketBoardConformance runs the ticket contract against a TicketBoard from
// newPort, which also returns the id of an existing project with no tickets.
func TicketBoardConformance(t *testing.T, newPort func(t *testing.T) (board ports.TicketBoard, projectID string)) {
	ctx := context.Background()

	t.Run("round trip", func(t *testing.T) {
		b, pid := newPort(t)
		created, err := b.CreateTicket(ctx, pid, "Ship it", "with care", domain.TicketStatusTodo, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := b.GetTicket(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Ship it" || got.Status != domain.TicketStatusTodo || got.ProjectID != pid {
			t.Fatalf("GetTicket = %+v, want the created ticket", got)
		}
		list, err := b.ListTickets(ctx, pid, ports.TicketFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != created.ID {
			t.Fatalf("ListTickets = %d tickets, want only %s", len(list), created.ID)
		}
		updated, err := b.UpdateTicket(ctx, created.ID, "Ship it now", "", domain.TicketStatusDone)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Title != "Ship it now" || updated.Status != domain.TicketStatusDone {
			t.Fatalf("UpdateTicket = %+v", updated)
		}
	})

	t.Run("error codes", func(t *testing.T) {
		b, pid := newPort(t)
		_, err := b.GetTicket(ctx, "missing")
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = b.CreateTicket(ctx, "missing", "T", "", "", "")
		wantCode(t, err, "PROJECT_NOT_FOUND")
		_, err = b.CreateTicket(ctx, pid, "", "", "", "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = b.CreateTicket(ctx, pid, "T", "", domain.TicketStatus("weird"), "")
		wantCode(t, err, "INVALID_STATUS")
	})

	t.Run("parent relation", func(t *testing.T) {
		b, pid := newPort(t)
		must := mustTicket(t)
		feature := must(b.CreateTicket(ctx, pid, "Feature", "", "", ""))
		if feature.ParentID != "" {
			t.Fatalf("a ticket created without a parent has parent %q", feature.ParentID)
		}
		child := must(b.CreateTicket(ctx, pid, "Child", "", "", feature.ID))
		if child.ParentID != feature.ID {
			t.Fatalf("CreateTicket with a parent = parent %q, want %s", child.ParentID, feature.ID)
		}
		if got := must(b.GetTicket(ctx, child.ID)); got.ParentID != feature.ID {
			t.Fatalf("GetTicket reads parent %q, want %s", got.ParentID, feature.ID)
		}
		loner := must(b.CreateTicket(ctx, pid, "Loner", "", "", ""))

		all, err := b.ListTickets(ctx, pid, ports.TicketFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 {
			t.Fatalf("ListTickets without a filter = %d tickets, want the feature, its child and the loner", len(all))
		}
		children, err := b.ListTickets(ctx, pid, ports.TicketFilter{ParentID: &feature.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(children) != 1 || children[0].ID != child.ID {
			t.Fatalf("ListTickets of the feature's children = %s, want only %s", ticketIDs(children), child.ID)
		}
		topLevel := ""
		top, err := b.ListTickets(ctx, pid, ports.TicketFilter{ParentID: &topLevel})
		if err != nil {
			t.Fatal(err)
		}
		if ids := ticketIDs(top); len(top) != 2 || !slices.Contains(ids, feature.ID) || !slices.Contains(ids, loner.ID) {
			t.Fatalf("ListTickets of the top-level tickets = %s, want the feature and the loner", ids)
		}

		// A full update keeps the parent; the patch moves it.
		if got := must(b.UpdateTicket(ctx, child.ID, "Child renamed", "", domain.TicketStatusTodo)); got.ParentID != feature.ID {
			t.Fatalf("UpdateTicket dropped the parent: %q", got.ParentID)
		}
		patcher, ok := b.(ports.TicketPatcher)
		if !ok {
			t.Skip("board is not a TicketPatcher")
		}
		if got := must(patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{ParentID: &feature.ID})); got.ParentID != feature.ID {
			t.Fatalf("PatchTicket to a parent = parent %q, want %s", got.ParentID, feature.ID)
		}
		title := "Loner renamed"
		if got := must(patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{Title: &title})); got.ParentID != feature.ID {
			t.Fatalf("a patch without parent_id dropped the parent: %q", got.ParentID)
		}
		if got := must(patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{ParentID: &topLevel})); got.ParentID != "" {
			t.Fatalf("PatchTicket with an empty parent_id kept the parent: %q", got.ParentID)
		}
	})

	t.Run("parent invalid", func(t *testing.T) {
		b, pid := newPort(t)
		must := mustTicket(t)
		feature := must(b.CreateTicket(ctx, pid, "Feature", "", "", ""))
		child := must(b.CreateTicket(ctx, pid, "Child", "", "", feature.ID))
		loner := must(b.CreateTicket(ctx, pid, "Loner", "", "", ""))

		_, err := b.CreateTicket(ctx, pid, "Orphan", "", "", "missing")
		wantCode(t, err, domain.CodeTicketParentInvalid)
		_, err = b.CreateTicket(ctx, pid, "Grandchild", "", "", child.ID)
		wantCode(t, err, domain.CodeTicketParentInvalid)

		patcher, ok := b.(ports.TicketPatcher)
		if !ok {
			t.Skip("board is not a TicketPatcher")
		}
		missing := "missing"
		_, err = patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{ParentID: &missing})
		wantCode(t, err, domain.CodeTicketParentInvalid)
		_, err = patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{ParentID: &loner.ID})
		wantCode(t, err, domain.CodeTicketParentInvalid)
		_, err = patcher.PatchTicket(ctx, loner.ID, ports.TicketPatch{ParentID: &child.ID})
		wantCode(t, err, domain.CodeTicketParentInvalid)
		// The feature has a child, so it cannot become a child itself.
		_, err = patcher.PatchTicket(ctx, feature.ID, ports.TicketPatch{ParentID: &loner.ID})
		wantCode(t, err, domain.CodeTicketParentInvalid)

		// Nothing above moved anything.
		for _, want := range []struct{ id, parent string }{{feature.ID, ""}, {child.ID, feature.ID}, {loner.ID, ""}} {
			if got := must(b.GetTicket(ctx, want.id)); got.ParentID != want.parent {
				t.Fatalf("ticket %s has parent %q after refused patches, want %q", want.id, got.ParentID, want.parent)
			}
		}
	})

	t.Run("children count and delete guard", func(t *testing.T) {
		b, pid := newPort(t)
		must := mustTicket(t)
		feature := must(b.CreateTicket(ctx, pid, "Feature", "", "", ""))
		first := must(b.CreateTicket(ctx, pid, "First", "", "", feature.ID))
		second := must(b.CreateTicket(ctx, pid, "Second", "", "", feature.ID))
		loner := must(b.CreateTicket(ctx, pid, "Loner", "", "", ""))

		// children_count is derived on every read: get and each list item.
		if got := must(b.GetTicket(ctx, feature.ID)); got.ChildrenCount != 2 {
			t.Fatalf("GetTicket of the feature counts %d children, want 2", got.ChildrenCount)
		}
		if got := must(b.GetTicket(ctx, first.ID)); got.ChildrenCount != 0 {
			t.Fatalf("GetTicket of a child counts %d children, want 0", got.ChildrenCount)
		}
		all, err := b.ListTickets(ctx, pid, ports.TicketFilter{})
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, tk := range all {
			counts[tk.ID] = tk.ChildrenCount
		}
		if counts[feature.ID] != 2 || counts[first.ID] != 0 || counts[second.ID] != 0 || counts[loner.ID] != 0 {
			t.Fatalf("ListTickets children counts = %v, want 2 on the feature and 0 elsewhere", counts)
		}

		// A feature task cannot be deleted while it has children.
		wantCode(t, b.DeleteTicket(ctx, feature.ID), domain.CodeTicketHasChildren)
		if _, err := b.GetTicket(ctx, feature.ID); err != nil {
			t.Fatalf("the refused delete removed the feature: %v", err)
		}
		if err := b.DeleteTicket(ctx, first.ID); err != nil {
			t.Fatalf("deleting a child: %v", err)
		}
		if got := must(b.GetTicket(ctx, feature.ID)); got.ChildrenCount != 1 {
			t.Fatalf("after deleting a child the feature counts %d children, want 1", got.ChildrenCount)
		}
		wantCode(t, b.DeleteTicket(ctx, feature.ID), domain.CodeTicketHasChildren)
		if err := b.DeleteTicket(ctx, second.ID); err != nil {
			t.Fatalf("deleting the last child: %v", err)
		}
		if err := b.DeleteTicket(ctx, feature.ID); err != nil {
			t.Fatalf("deleting the feature once its children are gone: %v", err)
		}
		_, err = b.GetTicket(ctx, feature.ID)
		wantCode(t, err, "TICKET_NOT_FOUND")
	})

	t.Run("deleted ticket is gone", func(t *testing.T) {
		b, pid := newPort(t)
		created, err := b.CreateTicket(ctx, pid, "T", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := b.DeleteTicket(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
		_, err = b.GetTicket(ctx, created.ID)
		wantCode(t, err, "TICKET_NOT_FOUND")
	})
}

// AgentCatalogConformance runs the agent contract against a fresh, empty
// AgentCatalog from newPort.
func AgentCatalogConformance(t *testing.T, newPort func(t *testing.T) ports.AgentCatalog) {
	ctx := context.Background()

	t.Run("round trip", func(t *testing.T) {
		c := newPort(t)
		created, err := c.CreateAgent(ctx, "reviewer", "reads diffs", "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.GetAgent(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "reviewer" || got.Description != "reads diffs" {
			t.Fatalf("GetAgent = %+v, want the created agent", got)
		}
		list, err := c.ListAgents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != created.ID {
			t.Fatalf("ListAgents = %d agents, want only %s", len(list), created.ID)
		}
	})

	t.Run("missing agent is AGENT_NOT_FOUND", func(t *testing.T) {
		c := newPort(t)
		_, err := c.GetAgent(ctx, "missing")
		wantCode(t, err, "AGENT_NOT_FOUND")
		wantCode(t, c.DeleteAgent(ctx, "missing"), "AGENT_NOT_FOUND")
	})

	t.Run("deleted agent is gone", func(t *testing.T) {
		c := newPort(t)
		created, err := c.CreateAgent(ctx, "reviewer", "", "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.DeleteAgent(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
		_, err = c.GetAgent(ctx, created.ID)
		wantCode(t, err, "AGENT_NOT_FOUND")
	})
}

// SessionReaderConformance runs the session-reading contract against a
// SessionReader from newPort, which also returns a session it has recorded.
func SessionReaderConformance(t *testing.T, newPort func(t *testing.T) (reader ports.SessionReader, recorded *domain.Session)) {
	ctx := context.Background()

	t.Run("get and list the recorded session", func(t *testing.T) {
		r, rec := newPort(t)
		got, err := r.Get(ctx, rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != rec.ID || got.ProjectID != rec.ProjectID || got.Status != rec.Status {
			t.Fatalf("Get = %+v, want %+v", got, rec)
		}
		list, err := r.List(ctx, ports.SessionFilter{ProjectID: rec.ProjectID})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != rec.ID {
			t.Fatalf("List = %d sessions, want only %s", len(list), rec.ID)
		}
	})

	t.Run("filters narrow the list", func(t *testing.T) {
		r, rec := newPort(t)
		list, err := r.List(ctx, ports.SessionFilter{ProjectID: "another-project"})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Fatalf("List for another project = %d sessions, want none", len(list))
		}
		list, err = r.List(ctx, ports.SessionFilter{Statuses: []domain.SessionStatus{rec.Status}})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("List by status %q = %d sessions, want 1", rec.Status, len(list))
		}
	})

	t.Run("missing session is SESSION_NOT_FOUND", func(t *testing.T) {
		r, _ := newPort(t)
		_, err := r.Get(ctx, "missing")
		wantCode(t, err, "SESSION_NOT_FOUND")
	})
}

// SessionFeedConformance runs the feed contract. newPort returns a feed, a
// project, and change, which changes one of the project's sessions and
// returns its id.
func SessionFeedConformance(t *testing.T, newPort func(t *testing.T) (feed ports.SessionFeed, projectID string, change func() string)) {
	t.Run("a change arrives with the session as it is now", func(t *testing.T) {
		feed, pid, change := newPort(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		changes, err := feed.FollowProject(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		id := change()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case c, ok := <-changes:
				if !ok {
					t.Fatal("the feed closed before the change arrived")
				}
				if c.Session.ID != id {
					continue
				}
				if c.Session.ProjectID != pid {
					t.Fatalf("change for %s carries project %q, want %q", id, c.Session.ProjectID, pid)
				}
				return
			case <-deadline:
				t.Fatalf("no change for %s", id)
			}
		}
	})

	t.Run("ending the follow closes the channel", func(t *testing.T) {
		feed, pid, _ := newPort(t)
		ctx, cancel := context.WithCancel(context.Background())
		changes, err := feed.FollowProject(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case _, ok := <-changes:
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("the channel stayed open after the follow ended")
			}
		}
	})

	t.Run("a project is required", func(t *testing.T) {
		feed, _, _ := newPort(t)
		_, err := feed.FollowProject(context.Background(), "")
		wantCode(t, err, "INVALID_INPUT")
	})
}
