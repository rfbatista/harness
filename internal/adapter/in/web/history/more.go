package history

import (
	"encoding/json"
	"net/http"

	"github.com/a-h/templ"
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
// stays open (the list grows beside it, and the address keeps naming it); a
// page that names none stays on base.
func keepOpen(base string, r *http.Request, open string) string {
	if named := r.PathValue("commit"); named != "" && named == open {
		return base + "/" + open
	}
	return base
}

// answerShowMore answers a "Show more" swap with the commits after the one
// it names, and reports whether r was one. When that commit is no longer
// listed (the history moved), there is nothing safe to append: 409, and the
// browser loads the page instead.
func (h Handler) answerShowMore(w http.ResponseWriter, r *http.Request, view PageView) (bool, error) {
	if r.Header.Get("HX-Target") != moreTarget {
		return false, nil
	}
	after := r.Header.Get(afterHeader)
	for i, row := range view.Rows {
		if after != "" && row.Hash == after {
			view.Rows = view.Rows[i+1:]
			return true, h.Render(w, r, http.StatusOK, nextCommits(view))
		}
	}
	http.Error(w, "the history changed since the page was read; reload it", http.StatusConflict)
	return true, nil
}
