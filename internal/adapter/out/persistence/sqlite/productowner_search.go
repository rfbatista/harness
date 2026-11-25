package sqlite

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
)

// The search of a project's conversations (ADR 0112) is an SQLite FTS5 table
// with the porter stemmer, filled as thread items are stored. The driver has
// FTS5 only when built with the sqlite_fts5 tag: without it the table cannot
// be made, the repository says so once at boot, and a search answers
// SEARCH_UNAVAILABLE while everything else works.

const searchTable = "product_owner_search"

// searchUnavailable is the answer of a search on a build without FTS5.
func searchUnavailable() error {
	return &domain.StructuredError{Code: "SEARCH_UNAVAILABLE",
		Message: "SEARCH_UNAVAILABLE: this server was built without SQLite FTS5 (the sqlite_fts5 build tag), so conversations cannot be searched"}
}

// prepareSearch makes the search table, and fills it from the items stored
// before it existed. It answers whether search works on this build.
func (r *ProductOwnerRepository) prepareSearch() bool {
	err := r.db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS " + searchTable +
		" USING fts5(item_id UNINDEXED, thread_id UNINDEXED, project_id UNINDEXED, body, tokenize = 'porter unicode61')").Error
	if err != nil {
		slog.Warn("conversation search is off: this build has no SQLite FTS5 (build with -tags sqlite_fts5)", "err", err)
		return false
	}
	// Items stored before the table existed, or by a build without FTS5, are
	// indexed now: every searchable item the table has no row for, by id.
	r.search = true // indexItem is a no-op until it is set
	r.backfillSearch()
	return true
}

// backfillSearch indexes the searchable items the search table lacks.
func (r *ProductOwnerRepository) backfillSearch() {
	var ms []ProductOwnerItemModel
	if err := r.db.Where("id NOT IN (SELECT item_id FROM "+searchTable+") AND "+
		"((json_extract(body, '$.kind') = ? AND COALESCE(json_extract(body, '$.text'), '') <> '' AND COALESCE(json_extract(body, '$.streaming'), 0) = 0) OR "+
		"(json_extract(body, '$.kind') = ? AND COALESCE(json_extract(body, '$.summary'), '') <> ''))",
		domain.ProductOwnerItemMessage, domain.ProductOwnerItemCheckpoint).Find(&ms).Error; err != nil {
		slog.Warn("index the stored thread items for search", "err", err)
		return
	}
	for _, m := range ms {
		if item, err := m.domain(); err == nil {
			r.indexItem(context.Background(), item)
		}
	}
}

// searchText is what of an item is searched: a message's text, a checkpoint's
// summary. An answer still streaming is indexed when it ends.
func searchText(item domain.ProductOwnerItem) string {
	switch {
	case item.Streaming:
		return ""
	case item.Kind == domain.ProductOwnerItemMessage:
		return item.Text
	case item.Kind == domain.ProductOwnerItemCheckpoint:
		return item.Summary
	}
	return ""
}

// indexItem puts an item's text in the search table, in place of what it had.
// A failure is logged: search is never a reason to lose a thread item.
func (r *ProductOwnerRepository) indexItem(ctx context.Context, item domain.ProductOwnerItem) {
	if !r.search {
		return
	}
	text := strings.TrimSpace(searchText(item))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM "+searchTable+" WHERE item_id = ?", item.ID).Error; err != nil {
			return err
		}
		if text == "" {
			return nil
		}
		return tx.Exec("INSERT INTO "+searchTable+" (item_id, thread_id, project_id, body) SELECT ?, ?, project_id, ? FROM "+
			ProductOwnerThreadModel{}.TableName()+" WHERE id = ?", item.ID, item.ThreadID, text, item.ThreadID).Error
	})
	if err != nil {
		slog.Warn("index a thread item for search", "item", item.ID, "err", err)
	}
}

// ftsQuery turns the person's words into an FTS5 query: each word quoted, so
// that no character of theirs is syntax, joined as "all of these"; the word OR
// between two words joins phrasings instead. It is "" for a query of no words.
func ftsQuery(q string) string {
	var parts []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if w == "OR" {
			// An OR joins two words; one with nothing before it is dropped.
			if len(parts) > 0 && parts[len(parts)-1] != "OR" {
				parts = append(parts, "OR")
			}
			continue
		}
		parts = append(parts, `"`+w+`"`)
	}
	for len(parts) > 0 && parts[len(parts)-1] == "OR" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " ")
}

// SearchConversations implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) SearchConversations(ctx context.Context, projectID, query string, limit int) ([]domain.ProductOwnerSearchMatch, error) {
	if !r.search {
		return nil, searchUnavailable()
	}
	match := ftsQuery(query)
	if match == "" {
		return []domain.ProductOwnerSearchMatch{}, nil
	}
	var rows []struct {
		ItemID   string
		ThreadID string
		Snippet  string
	}
	if err := r.db.WithContext(ctx).Raw("SELECT item_id, thread_id, snippet("+searchTable+", 3, '«', '»', '…', 24) AS snippet FROM "+searchTable+
		" WHERE "+searchTable+" MATCH ? AND project_id = ? ORDER BY rank LIMIT ?", match, projectID, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ProductOwnerSearchMatch, 0, len(rows))
	titles := map[string]string{}
	archivedUpTo := map[string]int64{}
	for _, row := range rows {
		var item ProductOwnerItemModel
		if err := r.db.WithContext(ctx).Where("id = ?", row.ItemID).Take(&item).Error; err != nil {
			continue // an item gone since it was indexed
		}
		var body struct {
			At time.Time `json:"at"`
		}
		_ = json.Unmarshal([]byte(item.Body), &body)
		if _, ok := titles[row.ThreadID]; !ok {
			var th ProductOwnerThreadModel
			if err := r.db.WithContext(ctx).Where("id = ?", row.ThreadID).Take(&th).Error; err != nil {
				continue
			}
			titles[row.ThreadID] = th.Title
			var upTo int64
			r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) FROM "+item.TableName()+" WHERE thread_id = ? AND json_extract(body, '$.kind') = ?",
				row.ThreadID, domain.ProductOwnerItemCheckpoint).Scan(&upTo)
			archivedUpTo[row.ThreadID] = upTo
		}
		out = append(out, domain.ProductOwnerSearchMatch{ThreadID: row.ThreadID, ThreadTitle: titles[row.ThreadID], ItemID: row.ItemID,
			At: body.At, Archived: item.Seq <= archivedUpTo[row.ThreadID], Snippet: row.Snippet})
	}
	return out, nil
}
