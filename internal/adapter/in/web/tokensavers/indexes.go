package tokensavers

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"operators-mcp/internal/domain"
)

// The Token savers page's Repository indexes section: each repository's
// codegraph index with its age and Rebuild index (ADR 0014). The server
// paints the list from ports.RepositoryIndexes (the same shape
// list_repository_indexes answers); the browser's repositoryIndexes takes it
// over and follows a build.

// IndexSeed is what repositoryIndexes starts from: GET
// /api/list_repository_indexes' answer. It is nil when the list could not
// be read; the browser then reads it itself.
type IndexSeed struct {
	Indexes []domain.RepositoryIndex `json:"repository_indexes"`
}

// IndexesView is the section as the server paints it.
type IndexesView struct {
	Seed *IndexSeed
	Now  time.Time
}

// readIndexes reads the section; nil when indexes are not served.
func (h Handler) readIndexes(r *http.Request) *IndexesView {
	if h.Indexes == nil {
		return nil
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	v := &IndexesView{Now: now()}
	list, err := h.Indexes.ListRepositoryIndexes(r.Context())
	if err != nil {
		slog.Warn("token savers page: repository indexes not read; the browser reads them", "err", err)
		return v
	}
	v.Seed = &IndexSeed{Indexes: list}
	return v
}

// IndexRow is one repository index as the server paints it before the
// browser takes over; the browser's domain/repository-index.js has the same
// rules.
type IndexRow struct {
	RepositoryID, Name, Checkout, State string
}

// Rows are the seed's indexes in order.
func (v IndexesView) Rows() []IndexRow {
	if v.Seed == nil {
		return nil
	}
	rows := make([]IndexRow, 0, len(v.Seed.Indexes))
	for _, idx := range v.Seed.Indexes {
		rows = append(rows, IndexRow{
			RepositoryID: idx.RepositoryID,
			Name:         IndexName(idx),
			Checkout:     idx.Checkout,
			State:        IndexState(idx, v.Now),
		})
	}
	return rows
}

// IndexName is "project / repository".
func IndexName(idx domain.RepositoryIndex) string {
	if idx.ProjectName == "" {
		return idx.RepositoryName
	}
	return idx.ProjectName + " / " + idx.RepositoryName
}

// IndexState says where the index stands: "Building…", "Built 3h ago",
// "Not built yet".
func IndexState(idx domain.RepositoryIndex, now time.Time) string {
	switch {
	case idx.Building:
		return "Building…"
	case idx.BuiltAt == nil:
		return "Not built yet"
	}
	return "Built " + age(*idx.BuiltAt, now)
}

// age mirrors relativeTime in web/src/shared/presentation/format.js, in
// words: "just now", "5m ago", "3h ago", "2d ago".
func age(then, now time.Time) string {
	d := max(now.Sub(then), 0)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}
