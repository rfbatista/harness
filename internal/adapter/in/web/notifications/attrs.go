package notifications

import (
	"net/url"

	"github.com/a-h/templ"
)

func selected(on bool) templ.Attributes {
	if on {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}

func checked(on bool) templ.Attributes {
	if on {
		return templ.Attributes{"checked": true}
	}
	return templ.Attributes{}
}

// moreHref is the next page without JavaScript: the same filter, older.
func moreHref(f Filter, before string) string {
	q := url.Values{}
	if f.ProjectID != "" {
		q.Set("project", f.ProjectID)
	}
	if f.Tab != "" {
		q.Set("tab", f.Tab)
	}
	q.Set("before", before)
	return Href + "?" + q.Encode()
}
