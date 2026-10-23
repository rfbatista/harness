package planning_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/blobstore"
	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// feedRecorder keeps what the service put on the project feed, as JSON.
type feedRecorder struct {
	mu      sync.Mutex
	changes []ports.ProjectChange
}

func (f *feedRecorder) AnnounceChange(c ports.ProjectChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *feedRecorder) wire(t *testing.T) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.changes))
	for i, c := range f.changes {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = string(b)
	}
	return out
}

type resourceFixture struct {
	svc      *planning.Service
	feed     *feedRecorder
	blobDir  string
	ticketID string
	events   []domain.Event
}

func newResourceFixture(t *testing.T) *resourceFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	f := &resourceFixture{feed: &feedRecorder{}, blobDir: t.TempDir()}
	bus := eventbus.New()
	ports.On(bus, func(_ context.Context, ev domain.TicketResourceAdded) error {
		f.events = append(f.events, ev)
		return nil
	})
	ports.On(bus, func(_ context.Context, ev domain.TicketCommentAdded) error {
		f.events = append(f.events, ev)
		return nil
	})
	f.svc = planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	f.svc.Resources, f.svc.Comments = sqlite.NewTicketResourceRepository(db), sqlite.NewTicketCommentRepository(db)
	f.svc.Blobs, f.svc.Feed, f.svc.Events = blobstore.New(f.blobDir), f.feed, bus
	f.svc.ContentURL = func(id string) string { return "/api/ticket_resources/" + id + "/content" }
	tk, err := f.svc.CreateTicket(context.Background(), p.ID, "Task", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f.ticketID = tk.ID
	return f
}

func (f *resourceFixture) upload(t *testing.T, up ports.ResourceUpload) *domain.TicketResource {
	t.Helper()
	if up.TicketID == "" {
		up.TicketID = f.ticketID
	}
	r, err := f.svc.UploadTicketResource(context.Background(), up)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func blobNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestUploadInfersTheMimeTypeWhenTheClientGaveNone(t *testing.T) {
	f := newResourceFixture(t)
	cases := []struct {
		name, fileName, declared, content, wantMime string
		wantKind                                    domain.ResourceKind
	}{
		{"declared wins", "x.bin", "Audio/MPEG", "ID3", "audio/mpeg", domain.ResourceAudio},
		{"declared params are kept", "x.txt", "text/plain; charset=ISO-8859-1", "hi", "text/plain; charset=ISO-8859-1", domain.ResourceFile},
		{"octet-stream falls back to the extension", "shot.png", "application/octet-stream", "not really", "image/png", domain.ResourceImage},
		{"no type, by extension", "doc.pdf", "", "%PDF-1.4", "application/pdf", domain.ResourceFile},
		{"no type, no known extension: sniffed", "blob", "", "\x89PNG\r\n\x1a\n", "image/png", domain.ResourceImage},
		{"sniffed text", "README", "", "plain words", "text/plain; charset=utf-8", domain.ResourceFile},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := f.upload(t, ports.ResourceUpload{FileName: c.fileName, Mime: c.declared, Content: strings.NewReader(c.content)})
			if r.Mime != c.wantMime || r.Kind != c.wantKind {
				t.Fatalf("mime %q kind %q, want %q %q", r.Mime, r.Kind, c.wantMime, c.wantKind)
			}
		})
	}
}

func TestUploadTitleDefaultsAndLimits(t *testing.T) {
	f := newResourceFixture(t)
	if r := f.upload(t, ports.ResourceUpload{FileName: `C:\fakepath\photo.jpg`, Content: strings.NewReader("x")}); r.Title != "photo.jpg" {
		t.Fatalf("title = %q", r.Title)
	}
	if r := f.upload(t, ports.ResourceUpload{FileName: "", Content: strings.NewReader("x")}); r.Title != "file" {
		t.Fatalf("a nameless upload is titled %q", r.Title)
	}
	_, err := f.svc.UploadTicketResource(context.Background(), ports.ResourceUpload{TicketID: f.ticketID, Title: strings.Repeat("x", 201), FileName: "a", Content: strings.NewReader("x")})
	if errs.Code(err) != "INVALID_INPUT" {
		t.Fatalf("over-long title = %v", err)
	}
}

func TestUploadKeepsTheBytesAndAPath(t *testing.T) {
	f := newResourceFixture(t)
	r := f.upload(t, ports.ResourceUpload{FileName: "a.txt", Mime: "text/plain", Content: strings.NewReader("hello")})
	if r.ContentURL != "/api/ticket_resources/"+r.ID+"/content" {
		t.Fatalf("content_url = %q", r.ContentURL)
	}
	p, err := f.svc.TicketResourcePath(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "hello" {
		t.Fatalf("bytes at %s = %q, %v", p, b, err)
	}
	link, _ := f.svc.AddTicketLink(context.Background(), f.ticketID, "https://x.io", "")
	if _, err := f.svc.TicketResourcePath(context.Background(), link.ID); errs.Code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("a link has no path: %v", err)
	}
}

func TestUploadOverTheCapLeavesNothing(t *testing.T) {
	f := newResourceFixture(t)
	f.svc.MaxResourceBytes = 10
	_, err := f.svc.UploadTicketResource(context.Background(), ports.ResourceUpload{TicketID: f.ticketID, FileName: "a", Content: bytes.NewReader(make([]byte, 11))})
	if errs.Code(err) != "RESOURCE_TOO_LARGE" {
		t.Fatalf("err = %v", err)
	}
	if names := blobNames(t, f.blobDir); len(names) != 0 {
		t.Fatalf("left behind %v", names)
	}
	if list, _ := f.svc.ListTicketResources(context.Background(), f.ticketID); len(list) != 0 {
		t.Fatalf("stored %+v", list)
	}
	if len(f.feed.wire(t)) != 0 {
		t.Fatalf("announced %v", f.feed.wire(t))
	}
}

func TestResourceChangesAreAnnouncedAndAdditionsRaiseEvents(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	r := f.upload(t, ports.ResourceUpload{FileName: "a.txt", Mime: "text/plain", Content: strings.NewReader("a")})
	link, err := f.svc.AddTicketLink(ctx, f.ticketID, "https://x.io", "X")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RetitleTicketResource(ctx, r.ID, "A"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RetitleTicketResource(ctx, r.ID, "A"); err != nil { // no change: no announcement
		t.Fatal(err)
	}
	if err := f.svc.DeleteTicketResource(ctx, link.ID); err != nil {
		t.Fatal(err)
	}
	wire := f.feed.wire(t)
	want := []string{`"ticket_resource":{"id":"` + r.ID, `"ticket_resource":{"id":"` + link.ID, `"title":"A"`,
		`{"ticket_resource":{"id":"` + link.ID + `","ticket_id":"` + f.ticketID + `","project_id":"`}
	if len(wire) != len(want) {
		t.Fatalf("feed = %v", wire)
	}
	for i := range want {
		if !strings.Contains(wire[i], want[i]) {
			t.Fatalf("change %d = %s, want it to contain %s", i, wire[i], want[i])
		}
	}
	if !strings.Contains(wire[3], `"deleted":true`) {
		t.Fatalf("deletion = %s", wire[3])
	}
	if len(f.events) != 2 {
		t.Fatalf("events = %+v, want only the two additions", f.events)
	}
	if ev, ok := f.events[0].(domain.TicketResourceAdded); !ok || ev.Resource.ID != r.ID || ev.Resource.ContentURL == "" {
		t.Fatalf("first event = %+v", f.events[0])
	}
	if ev, ok := f.events[1].(domain.TicketResourceAdded); !ok || ev.Resource.URL != "https://x.io" {
		t.Fatalf("second event = %+v", f.events[1])
	}
}

func TestDeletingAResourceRemovesItsBytes(t *testing.T) {
	f := newResourceFixture(t)
	r := f.upload(t, ports.ResourceUpload{FileName: "a.txt", Content: strings.NewReader("a")})
	if names := blobNames(t, f.blobDir); len(names) != 1 || names[0] != r.ID {
		t.Fatalf("blobs = %v", names)
	}
	if err := f.svc.DeleteTicketResource(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	if names := blobNames(t, f.blobDir); len(names) != 0 {
		t.Fatalf("blobs after delete = %v", names)
	}
}

func TestCommentChangesAreAnnouncedAndAdditionsRaiseEvents(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	c, err := f.svc.AddTicketComment(ctx, f.ticketID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateTicketComment(ctx, c.ID, "hello"); err != nil { // no change
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateTicketComment(ctx, c.ID, "hello again"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteTicketComment(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	wire := f.feed.wire(t)
	if len(wire) != 3 || !strings.Contains(wire[0], `"body":"hello"`) || !strings.Contains(wire[1], `"body":"hello again"`) ||
		!strings.Contains(wire[2], `"deleted":true`) || strings.Contains(wire[2], "body") {
		t.Fatalf("feed = %v", wire)
	}
	if len(f.events) != 1 {
		t.Fatalf("events = %+v", f.events)
	}
	if ev, ok := f.events[0].(domain.TicketCommentAdded); !ok || ev.Comment.ID != c.ID || ev.Comment.Body != "hello" {
		t.Fatalf("event = %+v", f.events[0])
	}
}

func TestDeletingATaskRemovesItsResourcesAndComments(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	r := f.upload(t, ports.ResourceUpload{FileName: "a.txt", Content: strings.NewReader("a")})
	if _, err := f.svc.AddTicketLink(ctx, f.ticketID, "https://x.io", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddTicketComment(ctx, f.ticketID, "x"); err != nil {
		t.Fatal(err)
	}
	before := len(f.feed.wire(t))
	if err := f.svc.DeleteTicket(ctx, f.ticketID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.GetTicketResource(ctx, r.ID); errs.Code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("resource survived: %v", err)
	}
	if names := blobNames(t, f.blobDir); len(names) != 0 {
		t.Fatalf("bytes survived: %v", names)
	}
	if wire := f.feed.wire(t)[before:]; len(wire) != 0 {
		t.Fatalf("the ticket's own deletion covers its items; announced %v", wire)
	}
}

func TestResourcesUnconfiguredAreUnavailable(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.ListTicketResources(context.Background(), "t"); errs.Code(err) != "UNAVAILABLE" {
		t.Fatalf("resources = %v", err)
	}
	if _, err := svc.ListTicketComments(context.Background(), "t", time.Time{}); errs.Code(err) != "UNAVAILABLE" {
		t.Fatalf("comments = %v", err)
	}
}

// gateReader blocks the upload mid-stream until released, so a test can
// delete the task while the bytes are being staged.
type gateReader struct {
	started chan struct{}
	release chan struct{}
	done    bool
}

func (g *gateReader) Read(p []byte) (int, error) {
	if g.done {
		return 0, io.EOF
	}
	close(g.started)
	<-g.release
	g.done = true
	p[0] = 'x'
	return 1, nil
}

// A task deleted while one of its uploads is staging keeps nothing of it:
// the upload is refused, and no record or bytes are left behind.
func TestUploadRacingATaskDeleteLeavesNothing(t *testing.T) {
	f := newResourceFixture(t)
	g := &gateReader{started: make(chan struct{}), release: make(chan struct{})}
	errc := make(chan error, 1)
	go func() {
		_, err := f.svc.UploadTicketResource(context.Background(), ports.ResourceUpload{TicketID: f.ticketID, FileName: "a", Content: g})
		errc <- err
	}()
	<-g.started
	if err := f.svc.DeleteTicket(context.Background(), f.ticketID); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	if err := <-errc; errs.Code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("upload to a task deleted meanwhile = %v", err)
	}
	if names := blobNames(t, f.blobDir); len(names) != 0 {
		t.Fatalf("left behind %v", names)
	}
	if len(f.events) != 0 {
		t.Fatalf("raised %+v for a deleted task", f.events)
	}
}
