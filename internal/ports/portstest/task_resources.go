package portstest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// pngHead is enough of a PNG for anything that sniffs it.
var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// TaskResourcesConformance runs the resources contract against a fresh port
// from newPort and a task of its project.
func TaskResourcesConformance(t *testing.T, newPort func(t *testing.T) (ports.TaskResources, string)) {
	ctx := context.Background()

	t.Run("upload, read back, retitle, delete", func(t *testing.T) {
		p, tid := newPort(t)
		up, err := p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tid, FileName: "shot.png", Mime: "image/png", Content: bytes.NewReader(pngHead)})
		if err != nil {
			t.Fatal(err)
		}
		if up.ID == "" || up.TicketID != tid || up.Kind != domain.ResourceImage || up.Title != "shot.png" ||
			up.Mime != "image/png" || up.SizeBytes != int64(len(pngHead)) || up.URL != "" {
			t.Fatalf("uploaded = %+v", up)
		}
		if list, err := p.ListTicketResources(ctx, tid); err != nil || len(list) != 1 || list[0].ID != up.ID || list[0].Title != "shot.png" {
			t.Fatalf("ListTicketResources = %+v, %v", list, err)
		}
		c, err := p.OpenTicketResource(ctx, up.ID)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(c.Content)
		c.Content.Close()
		if !bytes.Equal(body, pngHead) || c.Resource.ID != up.ID {
			t.Fatalf("content = %q (resource %+v)", body, c.Resource)
		}
		renamed, err := p.RetitleTicketResource(ctx, up.ID, "  Login screen  ")
		if err != nil || renamed.Title != "Login screen" {
			t.Fatalf("retitle = %+v, %v", renamed, err)
		}
		if err := p.DeleteTicketResource(ctx, up.ID); err != nil {
			t.Fatal(err)
		}
		wantCode(t, p.DeleteTicketResource(ctx, up.ID), "RESOURCE_NOT_FOUND")
		_, err = p.OpenTicketResource(ctx, up.ID)
		wantCode(t, err, "RESOURCE_NOT_FOUND")
		if list, _ := p.ListTicketResources(ctx, tid); len(list) != 0 {
			t.Fatalf("after delete = %+v", list)
		}
	})

	t.Run("a given title wins over the file name", func(t *testing.T) {
		p, tid := newPort(t)
		up, err := p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tid, Title: "Notes", FileName: "n.txt", Mime: "text/plain", Content: strings.NewReader("hi")})
		if err != nil || up.Title != "Notes" || up.Kind != domain.ResourceFile {
			t.Fatalf("upload = %+v, %v", up, err)
		}
	})

	t.Run("links, oldest first", func(t *testing.T) {
		p, tid := newPort(t)
		first, err := p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tid, FileName: "a.txt", Mime: "text/plain", Content: strings.NewReader("a")})
		if err != nil {
			t.Fatal(err)
		}
		link, err := p.AddTicketLink(ctx, tid, "https://example.com/spec", "")
		if err != nil {
			t.Fatal(err)
		}
		if link.Kind != domain.ResourceLink || link.URL != "https://example.com/spec" || link.Title != "https://example.com/spec" ||
			link.Mime != "" || link.SizeBytes != 0 || link.ContentURL != "" {
			t.Fatalf("link = %+v", link)
		}
		named, err := p.AddTicketLink(ctx, tid, "http://example.com", "Home")
		if err != nil || named.Title != "Home" {
			t.Fatalf("named link = %+v, %v", named, err)
		}
		list, err := p.ListTicketResources(ctx, tid)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 3 || list[0].ID != first.ID || list[1].ID != link.ID || list[2].ID != named.ID {
			t.Fatalf("list = %+v", list)
		}
		_, err = p.OpenTicketResource(ctx, link.ID)
		wantCode(t, err, "RESOURCE_NOT_FOUND")
	})

	t.Run("refusals", func(t *testing.T) {
		p, tid := newPort(t)
		_, err := p.ListTicketResources(ctx, "missing")
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: "missing", FileName: "a", Content: strings.NewReader("a")})
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.AddTicketLink(ctx, "missing", "https://x.io", "")
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tid, FileName: "empty.txt", Content: strings.NewReader("")})
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.AddTicketLink(ctx, tid, "ftp://x.io", "")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: tid, FileName: "big.bin",
			Content: io.LimitReader(zeros{}, domain.MaxResourceBytes+1)})
		wantCode(t, err, "RESOURCE_TOO_LARGE")
		link, err := p.AddTicketLink(ctx, tid, "https://x.io", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.RetitleTicketResource(ctx, link.ID, "   ")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.RetitleTicketResource(ctx, "missing", "x")
		wantCode(t, err, "RESOURCE_NOT_FOUND")
	})
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// TaskCommentsConformance runs the comments contract against a fresh port
// from newPort and a task of its project.
func TaskCommentsConformance(t *testing.T, newPort func(t *testing.T) (ports.TaskComments, string)) {
	ctx := context.Background()

	t.Run("add, list, edit, delete", func(t *testing.T) {
		p, tid := newPort(t)
		a, err := p.AddTicketComment(ctx, tid, "  first\nline two  ")
		if err != nil {
			t.Fatal(err)
		}
		if a.ID == "" || a.TicketID != tid || a.Author != domain.CommentByPerson || a.Body != "first\nline two" || !a.UpdatedAt.Equal(a.CreatedAt) {
			t.Fatalf("added = %+v", a)
		}
		b, err := p.AddTicketComment(ctx, tid, "second")
		if err != nil {
			t.Fatal(err)
		}
		list, err := p.ListTicketComments(ctx, tid, time.Time{})
		if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
			t.Fatalf("list = %+v, %v", list, err)
		}
		if since, _ := p.ListTicketComments(ctx, tid, b.CreatedAt); len(since) != 0 {
			t.Fatalf("since is strictly after: %+v", since)
		}
		same, err := p.UpdateTicketComment(ctx, a.ID, "first\nline two")
		if err != nil || !same.UpdatedAt.Equal(a.UpdatedAt) {
			t.Fatalf("a no-op edit keeps updated_at: %+v, %v", same, err)
		}
		edited, err := p.UpdateTicketComment(ctx, a.ID, "changed")
		if err != nil || edited.Body != "changed" || !edited.UpdatedAt.After(edited.CreatedAt) {
			t.Fatalf("edited = %+v, %v", edited, err)
		}
		if err := p.DeleteTicketComment(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		wantCode(t, p.DeleteTicketComment(ctx, a.ID), "COMMENT_NOT_FOUND")
		if list, _ := p.ListTicketComments(ctx, tid, time.Time{}); len(list) != 1 || list[0].ID != b.ID {
			t.Fatalf("after delete = %+v", list)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		p, tid := newPort(t)
		_, err := p.ListTicketComments(ctx, "missing", time.Time{})
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.AddTicketComment(ctx, "missing", "x")
		wantCode(t, err, "TICKET_NOT_FOUND")
		_, err = p.AddTicketComment(ctx, tid, " \n ")
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.AddTicketComment(ctx, tid, strings.Repeat("x", domain.MaxCommentLen+1))
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.UpdateTicketComment(ctx, "missing", "x")
		wantCode(t, err, "COMMENT_NOT_FOUND")
	})
}
