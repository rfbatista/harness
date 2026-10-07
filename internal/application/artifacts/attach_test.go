package artifacts

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeTickets reads tickets from the same database the links join.
type fakeTickets struct{ repo *sqlite.TicketRepository }

func (f fakeTickets) GetTicket(_ context.Context, id string) (*domain.Ticket, error) {
	if tk := f.repo.Get(id); tk != nil {
		return tk, nil
	}
	return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
}

func (f fakeTickets) ListTickets(_ context.Context, projectID string) ([]*domain.Ticket, error) {
	return f.repo.ListByProject(projectID), nil
}

// fakeSink records feed changes and, at the moment of each, what the
// repository held: a change must never run ahead of what is stored.
type fakeSink struct {
	mu      sync.Mutex
	repo    *sqlite.ArtifactRepository
	changes []ports.ProjectChange
	stored  []*domain.Artifact
}

func (s *fakeSink) AnnounceChange(c ports.ProjectChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes = append(s.changes, c)
	s.stored = append(s.stored, s.repo.Get(c.Artifact.Artifact.ID))
}

func (s *fakeSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.changes)
}

func (s *fakeSink) last(t *testing.T) (ports.ProjectChange, *domain.Artifact) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.changes) == 0 {
		t.Fatal("nothing announced on the feed")
	}
	return s.changes[len(s.changes)-1], s.stored[len(s.stored)-1]
}

type attachFixture struct {
	*fixture
	feed *fakeSink
	bus  *fakeBus
}

// newAttachFixture adds tasks tk1, tk2, tk3 of p1 and tk9 of p2, a ticket
// reader and a feed to the artifacts fixture.
func newAttachFixture(t *testing.T) *attachFixture {
	t.Helper()
	f := newFixture(t)
	db := f.db
	for _, tk := range []sqlite.TicketModel{
		{ID: "tk1", ProjectID: "p1", Title: "Design"}, {ID: "tk2", ProjectID: "p1", Title: "Board"},
		{ID: "tk3", ProjectID: "p1", Title: "Docs"}, {ID: "tk9", ProjectID: "p2", Title: "Elsewhere"},
	} {
		if err := db.Create(&tk).Error; err != nil {
			t.Fatal(err)
		}
	}
	af := &attachFixture{fixture: f, feed: &fakeSink{repo: f.repo}, bus: &fakeBus{}}
	f.svc.Tickets = fakeTickets{repo: sqlite.NewTicketRepository(db)}
	f.svc.Feed = af.feed
	f.svc.Subscribe(af.bus)
	return af
}

// asset publishes a page from s1 (task tk1) and moves it to the project.
func (f *attachFixture) asset(t *testing.T) *domain.Artifact {
	t.Helper()
	writeFile(t, f.root, "design/logo.html", "<p>logo</p>")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "design/logo.html", Title: "Logo"})
	return f.promote(t, a.ID)
}

func attached(a *domain.Artifact) string { return strings.Join(a.AttachedTicketIDs, ",") }

func TestAttach_LinksAndAnnouncesWhatIsStored(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	before := f.feed.count()

	got, err := f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	if err != nil {
		t.Fatal(err)
	}
	if attached(got) != "tk2" || got.Revision != a.Revision || !got.UpdatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("attached = %+v (was %+v)", got, a)
	}
	if f.feed.count() != before+1 {
		t.Fatalf("want one feed change, got %d", f.feed.count()-before)
	}
	c, stored := f.feed.last(t)
	if c.Deleted || attached(c.Artifact.Artifact) != "tk2" || attached(stored) != "tk2" {
		t.Fatalf("change = %+v, stored then = %+v", c.Artifact.Artifact, stored)
	}
	if list, _ := f.svc.ListTaskArtifacts(ctx, "tk2"); len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("tk2's artifacts = %d", len(list))
	}
}

func TestAttach_NoOpsAnnounceNothing(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	before := f.feed.count()
	for _, tk := range []string{"tk2", "tk1"} {
		got, err := f.svc.AttachArtifactToTicket(ctx, a.ID, tk)
		if err != nil || attached(got) != "tk2" {
			t.Fatalf("attach %s again = %+v %v", tk, got, err)
		}
	}
	got, err := f.svc.DetachArtifactFromTicket(ctx, a.ID, "tk3")
	if err != nil || attached(got) != "tk2" {
		t.Fatalf("detach a task not attached = %+v %v", got, err)
	}
	if f.feed.count() != before {
		t.Fatalf("no-ops announced %d changes", f.feed.count()-before)
	}
}

func TestAttachDetach_Refusals(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	writeFile(t, f.root, "design/draft.html", "draft")
	draft := f.publish(t, ports.PublishArtifactRequest{Path: "design/draft.html", Title: "Draft"})
	before := f.feed.count()

	for _, c := range []struct {
		name, artifact, ticket, code string
		detach                       bool
	}{
		{"blank artifact", "", "tk2", "INVALID_INPUT", false},
		{"blank ticket", a.ID, " ", "INVALID_INPUT", false},
		{"blank on detach", "", "", "INVALID_INPUT", true},
		{"no such artifact", "ghost", "tk2", "ARTIFACT_NOT_FOUND", false},
		{"no such task", a.ID, "ghost", "TICKET_NOT_FOUND", false},
		{"task of another project", a.ID, "tk9", "ARTIFACT_PROJECT_MISMATCH", false},
		{"task-scoped artifact", draft.ID, "tk2", "ARTIFACT_NOT_IN_PROJECT", false},
		{"detach the producer", a.ID, "tk1", "ARTIFACT_PRODUCER_TASK", true},
		{"detach from another project", a.ID, "tk9", "ARTIFACT_PROJECT_MISMATCH", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var err error
			if c.detach {
				_, err = f.svc.DetachArtifactFromTicket(ctx, c.artifact, c.ticket)
			} else {
				_, err = f.svc.AttachArtifactToTicket(ctx, c.artifact, c.ticket)
			}
			if errs.Code(err) != c.code {
				t.Fatalf("code %q (%v), want %s", errs.Code(err), err, c.code)
			}
		})
	}
	if f.feed.count() != before {
		t.Fatalf("refusals announced %d changes", f.feed.count()-before)
	}
}

func TestDetach_UnlinksAndAnnounces(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk3")
	got, err := f.svc.DetachArtifactFromTicket(ctx, a.ID, "tk2")
	if err != nil || attached(got) != "tk3" {
		t.Fatalf("detach = %+v %v", got, err)
	}
	if c, _ := f.feed.last(t); attached(c.Artifact.Artifact) != "tk3" {
		t.Fatalf("change = %+v", c.Artifact.Artifact)
	}
}

func TestScopeMoves_AreOnTheFeedAndMovingBackDropsLinks(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	if c, _ := f.feed.last(t); c.Artifact.Artifact.ID != a.ID || c.Artifact.Artifact.Scope != domain.ArtifactScopeProject {
		t.Fatalf("move to project change = %+v", c.Artifact.Artifact)
	}
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	back, err := f.svc.SetArtifactScope(ctx, a.ID, domain.ArtifactScopeTask)
	if err != nil || attached(back) != "" {
		t.Fatalf("move back = %+v %v", back, err)
	}
	c, _ := f.feed.last(t)
	if c.Artifact.Artifact.Scope != domain.ArtifactScopeTask || c.Artifact.Artifact.AttachedTicketIDs == nil || attached(c.Artifact.Artifact) != "" {
		t.Fatalf("move back change = %+v", c.Artifact.Artifact)
	}
	if list, _ := f.svc.ListTaskArtifacts(ctx, "tk2"); len(list) != 0 {
		t.Fatalf("tk2 keeps %d artifacts", len(list))
	}
}

func TestDelete_ProjectAssetLeavesItsIdsOnTheFeed(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	if err := f.svc.DeleteArtifact(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	c, stored := f.feed.last(t)
	if !c.Deleted || c.Artifact.Artifact.ID != a.ID || c.ProjectID() != "p1" || attached(c.Artifact.Artifact) != "tk2" || stored != nil {
		t.Fatalf("delete change = %+v %+v, stored %+v", c, c.Artifact.Artifact, stored)
	}

	writeFile(t, f.root, "design/draft.html", "draft")
	draft := f.publish(t, ports.PublishArtifactRequest{Path: "design/draft.html", Title: "Draft"})
	before := f.feed.count()
	f.svc.DeleteArtifact(ctx, draft.ID)
	if f.feed.count() != before {
		t.Fatal("deleting a task artifact is not a project change")
	}
}

func TestRepublish_ProjectAssetIsOnTheFeed(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	writeFile(t, f.root, "design/logo.html", "<p>logo v2</p>")
	f.publish(t, ports.PublishArtifactRequest{Path: "design/logo.html", Title: "Logo", Note: "v2"})
	c, _ := f.feed.last(t)
	if c.Artifact.Artifact.Revision != 2 || attached(c.Artifact.Artifact) != "tk2" {
		t.Fatalf("republish change = %+v", c.Artifact.Artifact)
	}

	before := f.feed.count()
	writeFile(t, f.root, "design/draft.html", "draft")
	f.publish(t, ports.PublishArtifactRequest{Path: "design/draft.html", Title: "Draft"})
	f.publish(t, ports.PublishArtifactRequest{Path: "design/draft.html", Title: "Draft"})
	if f.feed.count() != before {
		t.Fatal("task artifacts are not project changes")
	}
}

func TestTicketDeleted_DetachesAndAnnounces(t *testing.T) {
	f := newAttachFixture(t)
	a := f.asset(t)
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2")
	f.svc.AttachArtifactToTicket(ctx, a.ID, "tk3")
	before := f.feed.count()

	f.bus.fire(t, domain.TicketDeleted{TicketID: "tk2", ProjectID: "p1"})
	if f.feed.count() != before+1 {
		t.Fatalf("want one change, got %d", f.feed.count()-before)
	}
	if c, _ := f.feed.last(t); attached(c.Artifact.Artifact) != "tk3" {
		t.Fatalf("change = %+v", c.Artifact.Artifact)
	}
	// The producing task going leaves the asset in the project.
	f.bus.fire(t, domain.TicketDeleted{TicketID: "tk1", ProjectID: "p1"})
	if got, err := f.svc.GetArtifact(ctx, a.ID); err != nil || got.Scope != domain.ArtifactScopeProject {
		t.Fatalf("asset after its producer is deleted = %+v %v", got, err)
	}
}

// An attach racing a move back to task never leaves a link on a task artifact.
func TestAttach_RacingMoveToTask(t *testing.T) {
	for i := 0; i < 20; i++ {
		f := newAttachFixture(t)
		a := f.asset(t)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); f.svc.AttachArtifactToTicket(ctx, a.ID, "tk2") }()
		go func() { defer wg.Done(); f.svc.SetArtifactScope(ctx, a.ID, domain.ArtifactScopeTask) }()
		wg.Wait()
		got, _ := f.svc.GetArtifact(ctx, a.ID)
		if got.Scope == domain.ArtifactScopeTask && attached(got) != "" {
			t.Fatalf("task artifact attached to %s", attached(got))
		}
	}
}
