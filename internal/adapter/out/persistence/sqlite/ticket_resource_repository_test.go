package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

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

func TestTicketCommentRepo_KeepsTheWritingSession(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewTicketCommentRepository(db)
	c, err := r.Create(&domain.TicketComment{TicketID: "t", ProjectID: "p", Author: domain.CommentBySession, SessionID: "s1", AgentName: "go-developer", Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Author != domain.CommentBySession || c.SessionID != "s1" || c.AgentName != "go-developer" {
		t.Fatalf("created = %+v", c)
	}
	if got := r.Get(c.ID); got.Author != domain.CommentBySession || got.SessionID != "s1" || got.AgentName != "go-developer" {
		t.Fatalf("round trip = %+v", got)
	}
	if edited, err := r.SetBody(c.ID, "redone"); err != nil || edited.SessionID != "s1" || edited.AgentName != "go-developer" {
		t.Fatalf("SetBody keeps the author: %+v, %v", edited, err)
	}
}

func TestTicketCommentRepo_OldRowsReadBackAsThePersons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	old, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE ticket_comments (id text PRIMARY KEY, ticket_id text, project_id text, author text, body text, created_at integer, updated_at integer)`,
		`INSERT INTO ticket_comments VALUES ('c1', 't', 'p', 'person', 'hello', 1, 1)`,
		`INSERT INTO ticket_comments VALUES ('c2', 't', 'p', '', 'blank author', 2, 2)`,
	} {
		if err := old.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := old.DB()
	_ = sqlDB.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list := NewTicketCommentRepository(db).List("t", time.Time{})
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	for _, c := range list {
		if c.Author != domain.CommentByPerson || c.SessionID != "" || c.AgentName != "" {
			t.Errorf("old row %s = %+v, want a person's comment", c.ID, c)
		}
	}
}
