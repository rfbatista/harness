package sqlite

import (
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

func TestTicketResourceRepo(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTicketResourceRepository(db)
	a, err := r.Create(&domain.TicketResource{TicketID: "t", ProjectID: "p", Kind: domain.ResourceImage, Title: "Shot", Mime: "image/png", SizeBytes: 42})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.CreatedAt.IsZero() || !a.UpdatedAt.Equal(a.CreatedAt) {
		t.Fatalf("created = %+v", a)
	}
	b, _ := r.Create(&domain.TicketResource{TicketID: "t", ProjectID: "p", Kind: domain.ResourceLink, Title: "Docs", URL: "https://x.io"})
	other, _ := r.Create(&domain.TicketResource{TicketID: "other", ProjectID: "p", Kind: domain.ResourceFile, Title: "f"})

	list := r.ListByTicket("t")
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("oldest first, created in the same millisecond too: %+v", list)
	}
	if got := r.Get(a.ID); got.Mime != "image/png" || got.SizeBytes != 42 || got.Kind != domain.ResourceImage || got.ProjectID != "p" {
		t.Fatalf("round trip = %+v", got)
	}
	if r.Get("missing") != nil {
		t.Fatal("Get(missing) should be nil")
	}

	renamed, err := r.SetTitle(a.ID, "Screenshot")
	if err != nil || renamed.Title != "Screenshot" || !renamed.UpdatedAt.After(renamed.CreatedAt) {
		t.Fatalf("SetTitle = %+v, %v", renamed, err)
	}
	if _, err := r.SetTitle("missing", "x"); errs.Code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("SetTitle(missing) = %v", err)
	}

	if err := r.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(b.ID); errs.Code(err) != "RESOURCE_NOT_FOUND" {
		t.Fatalf("Delete twice = %v", err)
	}
	ids, err := r.DeleteByTicket("t")
	if err != nil || len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("DeleteByTicket = %v, %v", ids, err)
	}
	if len(r.ListByTicket("t")) != 0 || r.Get(other.ID) == nil {
		t.Fatal("DeleteByTicket must clear only its task")
	}
}

func TestTicketCommentRepo(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTicketCommentRepository(db)
	a, err := r.Create(&domain.TicketComment{TicketID: "t", ProjectID: "p", Author: domain.CommentByPerson, Body: "one\ntwo"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.Create(&domain.TicketComment{TicketID: "t", ProjectID: "p", Author: domain.CommentByPerson, Body: "three"})
	_, _ = r.Create(&domain.TicketComment{TicketID: "other", ProjectID: "p", Author: domain.CommentByPerson, Body: "x"})

	all := r.List("t", time.Time{})
	if len(all) != 2 || all[0].ID != a.ID || all[1].ID != b.ID {
		t.Fatalf("oldest first: %+v", all)
	}
	if got := r.Get(a.ID); got.Body != "one\ntwo" || got.Author != domain.CommentByPerson || got.ProjectID != "p" {
		t.Fatalf("round trip = %+v", got)
	}
	if got := r.List("t", a.CreatedAt.Add(-time.Millisecond)); len(got) != 2 {
		t.Fatalf("since before both = %d", len(got))
	}
	if got := r.List("t", b.CreatedAt); len(got) != 0 {
		t.Fatalf("since is strictly after: %+v", got)
	}

	edited, err := r.SetBody(a.ID, "edited")
	if err != nil || edited.Body != "edited" || !edited.UpdatedAt.After(edited.CreatedAt) {
		t.Fatalf("SetBody = %+v, %v", edited, err)
	}
	if _, err := r.SetBody("missing", "x"); errs.Code(err) != "COMMENT_NOT_FOUND" {
		t.Fatalf("SetBody(missing) = %v", err)
	}
	if err := r.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(a.ID); errs.Code(err) != "COMMENT_NOT_FOUND" {
		t.Fatalf("Delete twice = %v", err)
	}
	if err := r.DeleteByTicket("t"); err != nil {
		t.Fatal(err)
	}
	if len(r.List("t", time.Time{})) != 0 || len(r.List("other", time.Time{})) != 1 {
		t.Fatal("DeleteByTicket must clear only its task")
	}
}
