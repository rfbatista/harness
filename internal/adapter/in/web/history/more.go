package history

import (
	"encoding/json"
	"net/http"

	"github.com/a-h/templ"

	"operators-mcp/internal/domain"
)

// moreTarget is the id of the list's tail, which "Show more" swaps for the
// next commits; afterHeader carries the last commit the page shows.
const (
	moreTarget  = "history-more"
	afterHeader = "X-History-After"
)

// afterAttrs has "Show more" send the last commit shown, so the answer
// starts right after it.
func afterAttrs(rows []Row) templ.Attributes {
	if len(rows) == 0 {
		return nil
	}
	headers, _ := json.Marshal(map[string]string{afterHeader: rows[len(rows)-1].Hash})
	return templ.Attributes{"hx-headers": string(headers)}
}

// keepOpen is where "Show more" goes from base: the commit the page names
// (named, from its path) stays open, so the list grows beside it and the
// address keeps naming it; a page that names none stays on base.
func keepOpen(base, named, open string) string {
	if named != "" && named == open {
		return base + "/" + open
	}
	return base
}

// showMoreRequest reports whether htmx asks for the commits after those
// shown ("Show more"), which needs no open commit read.
func showMoreRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == moreTarget
}

// answerShowMore answers "Show more" with the commits after the one it
// names. When that commit is no longer listed (the history moved), there is
// nothing safe to append: 409, and the browser loads the page instead.
func (h Handler) answerShowMore(w http.ResponseWriter, r *http.Request, view PageView) error {
	after := r.Header.Get(afterHeader)
	for i, row := range view.Rows {
		if after != "" && row.Hash == after {
			view.Rows = view.Rows[i+1:]
			return h.Render(w, r, http.StatusOK, nextCommits(view))
		}
	}
	http.Error(w, "the history changed since the page was read; reload it", http.StatusConflict)
	return nil
}

// openHash is the open commit's full hash: as read when it was, as named
// otherwise (a "Show more" reads none).
func openHash(open string, detail *domain.CommitDetail) string {
	if detail != nil {
		return detail.Hash
	}
	return open
}
