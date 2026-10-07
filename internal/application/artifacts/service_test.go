package artifacts

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeAnnouncer struct{ events []ports.SessionEvent }

func (f *fakeAnnouncer) Announce(id string, ev ports.SessionEvent) {
	ev.SessionID = id
	f.events = append(f.events, ev)
}

// fakeBus records subscriptions so a test can fire a SessionDeleted by hand.
type fakeBus struct {
	handlers map[string][]ports.EventHandler
}

func (b *fakeBus) Subscribe(name string, h ports.EventHandler) {
	if b.handlers == nil {
		b.handlers = map[string][]ports.EventHandler{}
	}
	b.handlers[name] = append(b.handlers[name], h)
}

func (b *fakeBus) fire(t *testing.T, ev domain.Event) {
	t.Helper()
	for _, h := range b.handlers[ev.EventName()] {
		if err := h(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
}

type fixture struct {
	svc      *Service
	db       *gorm.DB
	repo     *sqlite.ArtifactRepository
	sessions *sqlite.SessionRepository
	ann      *fakeAnnouncer
	root     string // s1's worktree
	root2    string // s2's worktree (same task)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: db, repo: sqlite.NewArtifactRepository(db), sessions: sqlite.NewSessionRepository(db), ann: &fakeAnnouncer{}, root: t.TempDir(), root2: t.TempDir()}
	for _, s := range []*domain.Session{
		{ID: "s1", ProjectID: "p1", TicketID: "tk1", Task: "design", WorkingDir: f.root, Status: domain.SessionRunning, Interactive: true},
		{ID: "s2", ProjectID: "p1", TicketID: "tk1", Task: "design", WorkingDir: f.root2, Status: domain.SessionRunning},
		{ID: "s3", ProjectID: "p1", TicketID: "tk2", Task: "other", WorkingDir: t.TempDir(), Status: domain.SessionRunning},
	} {
		if _, err := f.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	f.svc = NewService(f.repo, f.sessions, f.ann)
	f.svc.StoreDir = t.TempDir()
	return f
}

func (f *fixture) publish(t *testing.T, req ports.PublishArtifactRequest) *domain.Artifact {
	t.Helper()
	if req.SessionID == "" {
		req.SessionID = "s1"
	}
	a, err := f.svc.Publish(context.Background(), req)
	if err != nil {
		t.Fatalf("publish %+v: %v", req, err)
	}
	return a
}

func TestPublish_PageInfersKindAndAnnounces(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "<!doctype html><h1>Card</h1>")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card", Note: "first cut"})

	if a.ID == "" || a.Kind != domain.ArtifactPage || a.Mime != "text/html" || a.Path != "design/card.html" || a.URL != "" ||
		a.Revision != 1 || a.SizeBytes != 28 || a.SessionID != "s1" || a.TicketID != "tk1" || a.ProjectID != "p1" || a.Note != "first cut" {
		t.Fatalf("artifact = %+v", a)
	}
	if len(f.ann.events) != 1 {
		t.Fatalf("want one announced event, got %d", len(f.ann.events))
	}
	ev := f.ann.events[0]
	if ev.Type != "artifact" || ev.SessionID != "s1" || ev.Text != "first cut" || ev.Artifact == nil || ev.Artifact.ID != a.ID || ev.At.IsZero() {
		t.Fatalf("event = %+v", ev)
	}
}

func TestPublish_RepublishBumpsRevisionKeepsID(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "v1")
	first := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	writeFile(t, f.root, "design/card.html", "v2 longer")
	second := f.publish(t, ports.PublishArtifactRequest{Path: "./design/card.html", Title: "Card, tighter", Note: "tighter"})

	if second.ID != first.ID || second.Revision != 2 || second.Title != "Card, tighter" || second.SizeBytes != 9 || !second.UpdatedAt.After(first.UpdatedAt) && !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("first %+v\nsecond %+v", first, second)
	}
	if f.ann.events[1].Artifact.Revision != 2 {
		t.Fatalf("second event carries revision %d", f.ann.events[1].Artifact.Revision)
	}
	// Another session publishing the same relative path gets its own artifact.
	writeFile(t, f.root2, "design/card.html", "theirs")
	other := f.publish(t, ports.PublishArtifactRequest{SessionID: "s2", Path: "design/card.html", Title: "Theirs"})
	if other.ID == first.ID || other.Revision != 1 {
		t.Fatalf("other session's artifact = %+v", other)
	}
}

func TestPublish_AbsolutePathInEitherRootSpelling(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/a.html", "<p>a</p>")
	rootReal, _ := filepath.EvalSymlinks(f.root)
	a := f.publish(t, ports.PublishArtifactRequest{Path: filepath.Join(f.root, "design", "a.html"), Title: "A"})
	b := f.publish(t, ports.PublishArtifactRequest{Path: filepath.Join(rootReal, "design", "a.html"), Title: "A"})
	if a.ID != b.ID || b.Revision != 2 || a.Path != "design/a.html" {
		t.Fatalf("both spellings must name one artifact: %+v / %+v", a, b)
	}
}

func TestPublish_PathErrors(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "x")
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(f.root, "design", "leak.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	for name, tc := range map[string]struct{ path, code string }{
		"missing":          {"design/nope.html", "ARTIFACT_NOT_FOUND"},
		"directory":        {"design", "ARTIFACT_NOT_FOUND"},
		"escapes, missing": {"../../etc/nope", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"absolute outside": {filepath.Join(outside, "secret.txt"), "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"symlink out":      {"design/leak.txt", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
		"git":              {".git/HEAD", "ARTIFACT_PATH_OUTSIDE_WORKTREE"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.Publish(context.Background(), ports.PublishArtifactRequest{SessionID: "s1", Path: tc.path, Title: "T"})
			if errs.Code(err) != tc.code {
				t.Fatalf("code = %q (%v), want %q", errs.Code(err), err, tc.code)
			}
		})
	}
	if len(f.ann.events) != 0 {
		t.Fatalf("a refused publish announced %d events", len(f.ann.events))
	}
}

func TestPublish_InputErrors(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "x")
	for name, tc := range map[string]struct {
		req  ports.PublishArtifactRequest
		code string
	}{
		"no title":        {ports.PublishArtifactRequest{SessionID: "s1", Path: "design/card.html"}, "INVALID_INPUT"},
		"neither target":  {ports.PublishArtifactRequest{SessionID: "s1", Title: "T"}, "INVALID_INPUT"},
		"both targets":    {ports.PublishArtifactRequest{SessionID: "s1", Title: "T", Path: "design/card.html", URL: "http://localhost:1"}, "INVALID_INPUT"},
		"bad kind":        {ports.PublishArtifactRequest{SessionID: "s1", Title: "T", Path: "design/card.html", Kind: "slide"}, "INVALID_INPUT"},
		"unknown session": {ports.PublishArtifactRequest{SessionID: "ghost", Title: "T", Path: "design/card.html"}, "SESSION_NOT_FOUND"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.Publish(context.Background(), tc.req)
			if errs.Code(err) != tc.code {
				t.Fatalf("code = %q (%v), want %q", errs.Code(err), err, tc.code)
			}
		})
	}
}

func TestPublish_TooLargeLeavesTheRecordAlone(t *testing.T) {
	f := newFixture(t)
	f.svc.MaxSizeBytes = 10
	writeFile(t, f.root, "clip.mp4", "12345")
	first := f.publish(t, ports.PublishArtifactRequest{Path: "clip.mp4", Title: "Clip"})
	writeFile(t, f.root, "clip.mp4", strings.Repeat("x", 11))
	_, err := f.svc.Publish(context.Background(), ports.PublishArtifactRequest{SessionID: "s1", Path: "clip.mp4", Title: "Clip"})
	if errs.Code(err) != "ARTIFACT_TOO_LARGE" || !strings.Contains(err.Error(), "10 B") || !strings.Contains(err.Error(), "11 B") {
		t.Fatalf("err = %v, want ARTIFACT_TOO_LARGE naming the cap and the size", err)
	}
	if got := f.repo.Get(first.ID); got.Revision != 1 || got.SizeBytes != 5 {
		t.Fatalf("refused publish changed the record: %+v", got)
	}
	if len(f.ann.events) != 1 {
		t.Fatalf("refused publish announced an event")
	}
}

func TestPublish_KindMismatch(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "<p>x</p>")
	writeFile(t, f.root, "notes.txt", "notes")
	for name, tc := range map[string]struct {
		req  ports.PublishArtifactRequest
		code string
		kind domain.ArtifactKind
	}{
		"image for html":     {ports.PublishArtifactRequest{Path: "design/card.html", Kind: "image"}, "ARTIFACT_KIND_MISMATCH", ""},
		"url for a path":     {ports.PublishArtifactRequest{Path: "design/card.html", Kind: "url"}, "ARTIFACT_KIND_MISMATCH", ""},
		"page for a url":     {ports.PublishArtifactRequest{URL: "http://localhost:3000", Kind: "page"}, "ARTIFACT_KIND_MISMATCH", ""},
		"file for html":      {ports.PublishArtifactRequest{Path: "design/card.html", Kind: "file"}, "", domain.ArtifactFile},
		"page for html":      {ports.PublishArtifactRequest{Path: "design/card.html", Kind: "page"}, "", domain.ArtifactPage},
		"omitted for txt":    {ports.PublishArtifactRequest{Path: "notes.txt"}, "", domain.ArtifactFile},
		"url kind for a url": {ports.PublishArtifactRequest{URL: "http://localhost:3000", Kind: "url"}, "", domain.ArtifactURL},
	} {
		t.Run(name, func(t *testing.T) {
			tc.req.SessionID, tc.req.Title = "s1", "T"
			a, err := f.svc.Publish(context.Background(), tc.req)
			if errs.Code(err) != tc.code {
				t.Fatalf("code = %q (%v), want %q", errs.Code(err), err, tc.code)
			}
			if tc.code == "" && a.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q", a.Kind, tc.kind)
			}
		})
	}
}

func TestPublish_URL(t *testing.T) {
	f := newFixture(t)
	a := f.publish(t, ports.PublishArtifactRequest{URL: "http://localhost:5173/", Title: "Dev server"})
	if a.Kind != domain.ArtifactURL || a.URL != "http://localhost:5173/" || a.Path != "" || a.Mime != "" || a.SizeBytes != 0 {
		t.Fatalf("url artifact = %+v", a)
	}
	again := f.publish(t, ports.PublishArtifactRequest{URL: "http://localhost:5173/", Title: "Dev server"})
	if again.ID != a.ID || again.Revision != 2 {
		t.Fatalf("re-publish = %+v", again)
	}
	for _, bad := range []string{"http://example.com", "http://10.0.0.1:3000", "http://localhost.evil.com", "file:///etc/passwd", "localhost:3000"} {
		_, err := f.svc.Publish(context.Background(), ports.PublishArtifactRequest{SessionID: "s1", URL: bad, Title: "T"})
		if errs.Code(err) != "ARTIFACT_URL_NOT_LOCAL" {
			t.Errorf("%q: code %q (%v)", bad, errs.Code(err), err)
		}
	}
}

func TestUnpublish_OnlyOwn(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "a.html", "a")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "a.html", Title: "A"})
	if err := f.svc.Unpublish(context.Background(), "s2", a.ID); errs.Code(err) != "ARTIFACT_NOT_YOURS" {
		t.Fatalf("other session unpublish = %v", err)
	}
	if err := f.svc.Unpublish(context.Background(), "s1", a.ID); err != nil {
		t.Fatal(err)
	}
	if f.repo.Get(a.ID) != nil {
		t.Fatal("record still there")
	}
	if _, err := os.Stat(filepath.Join(f.root, "a.html")); err != nil {
		t.Fatal("unpublish must never touch the file:", err)
	}
	if err := f.svc.Unpublish(context.Background(), "s1", a.ID); errs.Code(err) != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("second unpublish = %v", err)
	}
}

func TestList_TaskWideNewestFirst_AndFilters(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "a.html", "a")
	writeFile(t, f.root2, "b.html", "b")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "a.html", Title: "A"})
	time.Sleep(2 * time.Millisecond) // millisecond timestamps: keep the order unambiguous
	b := f.publish(t, ports.PublishArtifactRequest{SessionID: "s2", Path: "b.html", Title: "B"})
	time.Sleep(2 * time.Millisecond)
	writeFile(t, filepath.Dir(f.sessions.Get("s3").WorkingDir), filepath.Base(f.sessions.Get("s3").WorkingDir)+"/c.html", "c")
	f.publish(t, ports.PublishArtifactRequest{SessionID: "s3", Path: "c.html", Title: "C"})

	list, err := f.svc.ListTaskArtifacts(context.Background(), "tk1")
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("task list = %+v, %v", list, err)
	}
	bySession, _ := f.svc.ListArtifacts(context.Background(), ports.ArtifactFilter{SessionID: "s1"})
	if len(bySession) != 1 || bySession[0].ID != a.ID {
		t.Fatalf("session list = %+v", bySession)
	}
	if _, err := f.svc.ListArtifacts(context.Background(), ports.ArtifactFilter{}); errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("empty filter = %v", err)
	}
	if _, err := f.svc.GetArtifact(context.Background(), "ghost"); errs.Code(err) != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("ghost = %v", err)
	}
	if err := f.svc.DeleteArtifact(context.Background(), a.ID); err != nil || f.repo.Get(a.ID) != nil {
		t.Fatalf("delete = %v", err)
	}
}

func TestOpenArtifactFile_Confinement(t *testing.T) {
	f := newFixture(t)
	writeFile(t, f.root, "design/card.html", "<link rel=stylesheet href=style.css>")
	writeFile(t, f.root, "design/style.css", "h1{}")
	writeFile(t, f.root, "shared/tokens.css", ":root{}")
	writeFile(t, f.root, ".git/config", "[core]")
	outside := t.TempDir()
	writeFile(t, outside, "secret.txt", "s")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(f.root, "design", "leak.css")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	page := f.publish(t, ports.PublishArtifactRequest{Path: "design/card.html", Title: "Card"})
	link := f.publish(t, ports.PublishArtifactRequest{URL: "http://localhost:3000", Title: "Dev"})

	for name, tc := range map[string]struct {
		id, rel, mime, body string
		ok                  bool
	}{
		"the artifact itself":   {page.ID, "", "text/html", "<link rel=stylesheet href=style.css>", true},
		"sibling":               {page.ID, "style.css", "text/css", "h1{}", true},
		"up, still inside":      {page.ID, "../shared/tokens.css", "text/css", ":root{}", true},
		"escape":                {page.ID, "../../../../etc/passwd", "", "", false},
		"missing":               {page.ID, "nope.css", "", "", false},
		"directory":             {page.ID, "../shared", "", "", false},
		"symlink out":           {page.ID, "leak.css", "", "", false},
		"git":                   {page.ID, "../.git/config", "", "", false},
		"url artifact has none": {link.ID, "", "", "", false},
		"unknown artifact":      {"ghost", "", "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			af, err := f.svc.OpenArtifactFile(context.Background(), tc.id, tc.rel)
			if !tc.ok {
				if errs.Code(err) != "ARTIFACT_NOT_FOUND" {
					t.Fatalf("code = %q (%v), want ARTIFACT_NOT_FOUND", errs.Code(err), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer af.Content.Close()
			body, _ := io.ReadAll(af.Content)
			if af.Mime != tc.mime || string(body) != tc.body || af.Size != int64(len(tc.body)) || af.Revision != 1 || af.ModTime.IsZero() {
				t.Fatalf("file = %+v body %q", af, body)
			}
		})
	}
}

func TestSessionDeleted_RemovesRecords(t *testing.T) {
	f := newFixture(t)
	bus := &fakeBus{}
	f.svc.Subscribe(bus)
	writeFile(t, f.root, "a.html", "a")
	writeFile(t, f.root2, "b.html", "b")
	a := f.publish(t, ports.PublishArtifactRequest{Path: "a.html", Title: "A"})
	b := f.publish(t, ports.PublishArtifactRequest{SessionID: "s2", Path: "b.html", Title: "B"})

	bus.fire(t, domain.SessionDeleted{SessionID: "s1"})
	if f.repo.Get(a.ID) != nil || f.repo.Get(b.ID) == nil {
		t.Fatal("cascade removed the wrong records")
	}
	if _, err := os.Stat(filepath.Join(f.root, "a.html")); err != nil {
		t.Fatal("cascade must never touch files:", err)
	}
}
