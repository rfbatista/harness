package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// followedUsage is a usage reader whose feed is a channel the test drives;
// it records which projects were followed and when each follower's ctx ends.
type followedUsage struct {
	ports.UsageReader
	ch       chan *domain.UsageRecord
	followed chan string
	released chan struct{}
}

func newFollowedUsage() *followedUsage {
	return &followedUsage{ch: make(chan *domain.UsageRecord, 4), followed: make(chan string, 4), released: make(chan struct{}, 4)}
}

func (f *followedUsage) FollowUsage(ctx context.Context, projectID string) (<-chan *domain.UsageRecord, error) {
	f.followed <- projectID
	go func() { <-ctx.Done(); f.released <- struct{}{} }()
	return f.ch, nil
}

// followedCatalog is a project catalog feed the test drives, recording when
// it is followed and released.
type followedCatalog struct {
	ch       chan ports.ProjectCatalogChange
	followed chan struct{}
	released chan struct{}
}

func newFollowedCatalog() *followedCatalog {
	return &followedCatalog{ch: make(chan ports.ProjectCatalogChange, 4), followed: make(chan struct{}, 4), released: make(chan struct{}, 4)}
}

func (f *followedCatalog) FollowProjects(ctx context.Context) (<-chan ports.ProjectCatalogChange, error) {
	f.followed <- struct{}{}
	go func() { <-ctx.Done(); f.released <- struct{}{} }()
	return f.ch, nil
}

// envelopeOf splits a page event stream message into its kind and the one
// other key it carries.
func envelopeOf(t *testing.T, raw, key string) (kind, payload string) {
	t.Helper()
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 {
		t.Fatalf("envelope = %s, want kind and %s only", raw, key)
	}
	_ = json.Unmarshal(env["kind"], &kind)
	return kind, string(env[key])
}

func TestAPageEventStreamWithUsageAndProjectsCarriesTheirOldMessages(t *testing.T) {
	cost := 0.25
	record := &domain.UsageRecord{ID: "u1", ProjectID: "p1", Model: "claude", InputTokens: 10, StartedAt: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), EstimatedCostUSD: &cost}
	catalog := ports.ProjectCatalogChange{Project: &domain.Project{ID: "p1", Name: "widgets"}}

	oldUsage, oldCatalog := newFollowedUsage(), newFollowedCatalog()
	oldUsage.ch <- record
	oldCatalog.ch <- catalog
	old := &Handler{usage: oldUsage, projectFeed: oldCatalog}
	wantUsage := oldPayload(t, old, "/api/projects/p1/usage/events")
	wantCatalog := oldPayload(t, old, "/api/project_events")

	usage, feed, creds := newFollowedUsage(), newFollowedCatalog(), gitcredentialstest.New()
	h := &Handler{usage: usage, projectFeed: feed, gitCredentials: creds}
	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1&with=usage,bogus,projects")
	s.hello()
	select {
	case got := <-usage.followed:
		if got != "p1" {
			t.Fatalf("followed usage of %q, want p1", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("usage not followed")
	}
	waitFor(t, "project catalog followed", feed.followed)

	usage.ch <- record
	if kind, payload := envelopeOf(t, s.data(), "record"); kind != "usage" || payload != wantUsage {
		t.Fatalf("usage envelope = %s %s, want record %s", kind, payload, wantUsage)
	}
	feed.ch <- catalog
	if kind, payload := envelopeOf(t, s.data(), "change"); kind != "projects" || payload != wantCatalog {
		t.Fatalf("projects envelope = %s %s, want change %s", kind, payload, wantCatalog)
	}

	// The credential changes /api/project_events carries ride along too.
	oldStream, _ := openSSE(t, ctx, srv.URL+"/api/project_events")
	waitFor(t, "old stream following the catalog", feed.followed)
	if _, err := creds.UpdateGitIdentity(context.Background(), "Ada", "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	wantIdentity := oldStream.data()
	if kind, payload := envelopeOf(t, s.data(), "change"); kind != "projects" || payload != wantIdentity {
		t.Fatalf("projects envelope = %s %s, want change %s", kind, payload, wantIdentity)
	}
}

func TestAPageEventStreamCarriesUsageAndProjectsOnlyWhenAskedTo(t *testing.T) {
	for _, c := range []struct{ name, query string }{
		{"without with", "?project_id=p1"},
		{"unknown names only", "?project_id=p1&with=bogus"},
	} {
		t.Run(c.name, func(t *testing.T) {
			usage, feed, notes := newFollowedUsage(), newFollowedCatalog(), newFollowedInbox()
			h := &Handler{usage: usage, projectFeed: feed, notifications: notes}
			srv := httptest.NewServer(NewRouter(h))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, _ := openSSE(t, ctx, srv.URL+"/api/page_events"+c.query)
			s.hello()

			notes.ch <- ports.NotificationChange{Changed: []*domain.Notification{}, UnreadCount: 1}
			if raw := s.data(); !strings.Contains(raw, `"kind":"notifications"`) {
				t.Fatalf("message = %s, want an inbox change", raw)
			}
			select {
			case p := <-usage.followed:
				t.Fatalf("followed usage of %q without with=usage", p)
			case <-feed.followed:
				t.Fatal("followed the project catalog without with=projects")
			default:
			}
		})
	}
}

func TestAPageEventStreamOutsideAProjectIgnoresUsage(t *testing.T) {
	usage, feed := newFollowedUsage(), newFollowedCatalog()
	srv := httptest.NewServer(NewRouter(&Handler{usage: usage, projectFeed: feed}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?with=usage,projects")
	s.hello()
	waitFor(t, "project catalog followed", feed.followed)

	feed.ch <- ports.ProjectCatalogChange{Project: &domain.Project{ID: "p1"}}
	if kind, _ := envelopeOf(t, s.data(), "change"); kind != "projects" {
		t.Fatalf("kind = %s, want projects", kind)
	}
	select {
	case p := <-usage.followed:
		t.Fatalf("followed usage of %q with no project_id", p)
	default:
	}
}

func TestAPageEventStreamEndsWhenUsageOrProjectsFallsBehind(t *testing.T) {
	for _, part := range []string{"usage", "projects"} {
		t.Run(part, func(t *testing.T) {
			usage, feed := newFollowedUsage(), newFollowedCatalog()
			h := &Handler{usage: usage, projectFeed: feed}
			srv := httptest.NewServer(NewRouter(h))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, _ := openSSE(t, ctx, srv.URL+"/api/page_events?project_id=p1&with=usage,projects")
			id := s.hello()

			if part == "usage" {
				close(usage.ch)
			} else {
				close(feed.ch)
			}
			s.ended()
			waitFor(t, "usage follower released", usage.released)
			waitFor(t, "catalog follower released", feed.released)
			waitUntil(t, "stream leaving the registry", func() bool { _, ok := h.pageStreams.lookup(id); return !ok })
		})
	}
}
