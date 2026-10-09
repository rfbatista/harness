package tooling

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func (f *taskToolsFixture) uploadResource(t *testing.T, ticketID, fileName, mime string, content []byte) *domain.TicketResource {
	t.Helper()
	r, err := f.plan.UploadTicketResource(context.Background(), ports.ResourceUpload{TicketID: ticketID, FileName: fileName, Mime: mime, Content: bytes.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *taskToolsFixture) readResource(t *testing.T, id string) map[string]any {
	t.Helper()
	out, err := f.call(t, f.sessionID, "read_task_resource", map[string]any{"resource_id": id})
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)
}

func TestListTaskResources(t *testing.T) {
	f := newTaskToolsFixture(t)
	a := f.uploadResource(t, f.ticketID, "a.txt", "text/plain", []byte("a"))
	link, _ := f.plan.AddTicketLink(context.Background(), f.ticketID, "https://x.io", "")
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Other", "", domain.TicketStatusTodo)
	f.uploadResource(t, other.ID, "b.txt", "text/plain", []byte("b"))

	out, err := f.call(t, f.sessionID, "list_task_resources", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	list := m["resources"].([]*domain.TicketResource)
	if m["count"] != 2 || len(list) != 2 || list[0].ID != a.ID || list[1].ID != link.ID {
		t.Fatalf("list = %+v", m)
	}
}

func TestReadTaskResource_TextInline(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, c := range []struct{ name, mime string }{
		{"notes.md", "text/markdown"}, {"data.json", "application/json"}, {"feed.xml", "application/xml"},
		{"x.geojson", "application/geo+json"}, {"x.svg", "image/svg+xml"}, {"t.csv", "text/csv"},
	} {
		t.Run(c.mime, func(t *testing.T) {
			r := f.uploadResource(t, f.ticketID, c.name, c.mime, []byte("body of "+c.name))
			m := f.readResource(t, r.ID)
			if m["content"] != "body of "+c.name {
				t.Fatalf("content = %#v", m["content"])
			}
			path, _ := m["local_path"].(string)
			if b, err := os.ReadFile(path); err != nil || string(b) != "body of "+c.name {
				t.Fatalf("local_path %q = %q, %v", path, b, err)
			}
		})
	}
}

func TestReadTaskResource_LargeOrBinaryGoesByPath(t *testing.T) {
	f := newTaskToolsFixture(t)
	big := f.uploadResource(t, f.ticketID, "big.txt", "text/plain", bytes.Repeat([]byte("x"), 256<<10+1))
	m := f.readResource(t, big.ID)
	if _, ok := m["content"]; ok {
		t.Fatal("over 256 KiB must not be inlined")
	}
	if !strings.Contains(m["note"].(string), "local_path") || m["local_path"] == "" {
		t.Fatalf("note = %q", m["note"])
	}
	exact := f.uploadResource(t, f.ticketID, "exact.txt", "text/plain", bytes.Repeat([]byte("x"), 256<<10))
	if m := f.readResource(t, exact.ID); m["content"] == nil {
		t.Fatal("exactly 256 KiB is inlined")
	}

	img := f.uploadResource(t, f.ticketID, "shot.png", "image/png", []byte("\x89PNG\r\n\x1a\n"))
	m = f.readResource(t, img.ID)
	if _, ok := m["content"]; ok || !strings.Contains(m["note"].(string), "Read tool") {
		t.Fatalf("image = %+v", m)
	}
	audio := f.uploadResource(t, f.ticketID, "voice.mp3", "audio/mpeg", []byte("ID3"))
	if m := f.readResource(t, audio.ID); !strings.Contains(m["note"].(string), "transcri") {
		t.Fatalf("audio note = %q", m["note"])
	}
	bad := f.uploadResource(t, f.ticketID, "bad.txt", "text/plain", []byte{0xff, 0xfe, 0x00})
	if m := f.readResource(t, bad.ID); m["content"] != nil {
		t.Fatal("text that is not UTF-8 goes by path")
	}
	if m := f.readResource(t, img.ID); !strings.Contains(m["note"].(string), "another machine") {
		t.Fatalf("the note must say where the path is: %q", m["note"])
	}
}

func TestReadTaskResource_Link(t *testing.T) {
	f := newTaskToolsFixture(t)
	link, _ := f.plan.AddTicketLink(context.Background(), f.ticketID, "https://x.io/spec", "Spec")
	m := f.readResource(t, link.ID)
	if m["resource"].(*domain.TicketResource).URL != "https://x.io/spec" {
		t.Fatalf("resource = %+v", m["resource"])
	}
	if _, ok := m["local_path"]; ok {
		t.Fatal("a link has no local_path")
	}
	if _, ok := m["content"]; ok {
		t.Fatal("a link has no content")
	}
	if !strings.Contains(m["note"].(string), "never fetches") {
		t.Fatalf("note = %q", m["note"])
	}
}

func TestReadTaskResource_Refusals(t *testing.T) {
	f := newTaskToolsFixture(t)
	other, _ := f.plan.CreateTicket(context.Background(), f.projectID, "Other", "", domain.TicketStatusTodo)
	theirs := f.uploadResource(t, other.ID, "b.txt", "text/plain", []byte("b"))
	_, err := f.call(t, f.sessionID, "read_task_resource", map[string]any{"resource_id": theirs.ID})
	wantCode(t, err, "RESOURCE_NOT_ON_TASK")
	_, err = f.call(t, f.sessionID, "read_task_resource", map[string]any{"resource_id": "missing"})
	wantCode(t, err, "RESOURCE_NOT_FOUND")
	_, err = f.call(t, f.sessionID, "read_task_resource", map[string]any{})
	wantCode(t, err, "INVALID_INPUT")
}

func TestListTaskComments(t *testing.T) {
	f := newTaskToolsFixture(t)
	ctx := context.Background()
	a, _ := f.plan.AddTicketComment(ctx, f.ticketID, "first")
	time.Sleep(2 * time.Millisecond)
	b, _ := f.plan.AddTicketComment(ctx, f.ticketID, "second")
	other, _ := f.plan.CreateTicket(ctx, f.projectID, "Other", "", domain.TicketStatusTodo)
	_, _ = f.plan.AddTicketComment(ctx, other.ID, "elsewhere")

	out, err := f.call(t, f.sessionID, "list_task_comments", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if list := m["comments"].([]*domain.TicketComment); m["count"] != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("list = %+v", m)
	}
	out, err = f.call(t, f.sessionID, "list_task_comments", map[string]any{"since": a.CreatedAt.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	if list := out.(map[string]any)["comments"].([]*domain.TicketComment); len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("since is strictly after: %+v", list)
	}
	_, err = f.call(t, f.sessionID, "list_task_comments", map[string]any{"since": "yesterday"})
	wantCode(t, err, "INVALID_INPUT")
}

func TestResourceToolsUnavailableWithoutTheirPorts(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, tl := range SessionTaskTools(f.plan, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, ResourceTooling{}, NotificationTooling{}) {
		if tl.Name != "list_task_resources" && tl.Name != "list_task_comments" {
			continue
		}
		_, err := tl.Handler(WithSessionID(context.Background(), f.sessionID), nil)
		wantCode(t, err, "UNAVAILABLE")
	}
}
