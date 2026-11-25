//go:build sqlite_fts5

package sqlite

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The boot indexes every searchable item the table has no row for, found by id:
// a table with as many rows as there are items can still lack one, when a row
// outlives its item.
func TestTheBootIndexesTheItemsTheSearchTableLacksByIdNotByCount(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := NewProductOwnerRepository(db)
	ctx := context.Background()
	if err := r.CreateThread(ctx, domain.ProductOwnerThread{ID: "t1", ProjectID: "p1", Title: "t", Turn: domain.ProductOwnerTurnIdle, StartedAt: time.Now(), LastActivityAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, text := range []string{"first quokka", "second wombat"} {
		item, err := r.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: "t1", Kind: domain.ProductOwnerItemMessage, At: time.Now(), Author: domain.ProductOwnerAuthorPerson, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, item.ID)
	}
	// The second item loses its row and a row of no item takes its place: the
	// counts agree.
	if err := db.Exec("DELETE FROM "+searchTable+" WHERE item_id = ?", ids[1]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO " + searchTable + " (item_id, thread_id, project_id, body) VALUES ('ghost', 't1', 'p1', 'ghost')").Error; err != nil {
		t.Fatal(err)
	}

	r = NewProductOwnerRepository(db)
	got, err := r.SearchConversations(ctx, "p1", "wombat", 30)
	if err != nil || len(got) != 1 || got[0].ItemID != ids[1] {
		t.Errorf("search for the lost item = %+v %v, want its item %s", got, err, ids[1])
	}
}
