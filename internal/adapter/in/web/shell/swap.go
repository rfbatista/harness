package shell

import (
	"net/http"

	"github.com/a-h/templ"
)

// SwapRequest reports whether htmx asked a page's URL for its swap region (a
// fragment response) rather than the full page (ADR 0008). Either answer
// varies on HX-Request, so it marks the response that way.
func SwapRequest(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Add("Vary", "HX-Request")
	return r.Header.Get("HX-Request") == "true"
}

// SwapRegion marks the element with id as a swap region: the web client
// shows it busy, focuses its heading and scrolls it to the top on each swap.
func SwapRegion(id string) templ.Attributes {
	return templ.Attributes{"id": id, "data-swap-region": true}
}

// SwapLink makes a real link swap the region with id in place: htmx boosts
// that link alone, so the address bar follows and a modified click still
// opens a new tab.
func SwapLink(region string) templ.Attributes {
	return templ.Attributes{"hx-boost": "true", "hx-target": "#" + region, "hx-swap": "outerHTML", "hx-push-url": "true"}
}
