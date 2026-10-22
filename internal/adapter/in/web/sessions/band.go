package sessions

import "net/http"

// BandCookie holds the context strip's open panel: "reviews", "feature",
// "token-savers", or "none" for a person who closed it. The browser writes it
// when a summary button is pressed; the task page reads it so the first paint
// already has that panel open. One preference per browser, for every task.
const BandCookie = "task_band"

// ParseBand reads the cookie's value: the panel's key, or "" when it is
// unset, unknown or malformed.
func ParseBand(value string) string {
	switch value {
	case "reviews", "feature", "token-savers", "none":
		return value
	}
	return ""
}

// BandFrom is the open panel the request's cookie names; "" when none does.
func BandFrom(r *http.Request) string {
	c, err := r.Cookie(BandCookie)
	if err != nil {
		return ""
	}
	return ParseBand(c.Value)
}

// OpenBand is the panel the server paints open ("" for none): the stored
// choice when this task has that band, else the default: Reviews while a
// request waits. The browser reaches the same answer (tasksContextStrip);
// only a Reviews button that holds settled requests alone is beyond the
// server, which does not read them.
func (v PageView) OpenBand() string {
	switch v.Band {
	case "none":
		return ""
	case "reviews":
		if v.PendingReviews > 0 {
			return "reviews"
		}
		return ""
	case "feature":
		if v.Feature != nil {
			return "feature"
		}
	case "token-savers":
		if v.TokenSavers != nil {
			return "token-savers"
		}
	}
	if v.PendingReviews > 0 {
		return "reviews"
	}
	return ""
}
