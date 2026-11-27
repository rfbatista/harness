package httpapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/ports"
)

// Person-only routes (ADR 0016): a route a session must not call, because it
// answers a request that guards it or loosens the Guard layer, accepts only a
// person credential, as the cookie the browser got when it paired, or as the
// header a client such as the TUI sends. Nothing else is proof: not the bearer
// API token, not a loopback Host or Origin, not any marker a session sets.

const (
	// PersonCookie is the cookie a paired browser holds.
	PersonCookie = "harness_person"
	// PersonHeader carries the credential for a client that is not a browser.
	PersonHeader = "X-Harness-Person"
	// PairPath redeems a pairing code: GET sets the cookie and goes to the
	// UI, POST answers the credential for a client.
	PairPath = "/api/person/pair"
	// pairCookieMaxAge is how long the browser keeps its credential: ten years, so
	// it is paired once; the credential can be revoked by wiping the store.
	pairCookieMaxAge = 10 * 365 * 24 * 3600
	// PairingCodesPath issues another pairing code, for another browser or client.
	PairingCodesPath = "/api/person/pairing-codes"
)

// WithPersonProof makes the router enforce person-only routes: a request to one
// without a credential the verifier accepts is refused with PERSON_ONLY, and
// refusals reports each. The server always passes it, so every route a session
// can reach enforces; a router built without it (a unit test of one route) does
// not.
func WithPersonProof(v ports.PersonVerifier, refusals ports.PersonRefusals) Option {
	return func(o *options) { o.person, o.refusals = v, refusals }
}

// personOnly is the middleware of a person-only route.
func personOnly(o options) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if o.person == nil {
				return next(c)
			}
			if o.person.Verify(c.Request().Context(), presentedCredential(c.Request())) {
				return next(c)
			}
			if o.refusals != nil {
				r := ports.PersonRefusal{Method: c.Request().Method, Route: c.Path(), ProjectID: c.Param("project")}
				if strings.HasPrefix(c.Path(), "/api/sessions/:id") {
					r.SessionID = c.Param("id")
				}
				o.refusals.Refused(c.Request().Context(), r)
			}
			return refused("PERSON_ONLY", "PERSON_ONLY: this action is the person's: it needs the credential their browser or client got by pairing with the harness. A session cannot take it.")
		}
	}
}

func presentedCredential(r *http.Request) string {
	if v := r.Header.Get(PersonHeader); v != "" {
		return v
	}
	if ck, err := r.Cookie(PersonCookie); err == nil {
		return ck.Value
	}
	return ""
}

// handlePairGet redeems the pairing code in the link the harness printed: the
// browser gets the credential as an HttpOnly cookie that is never sent to
// another site, and goes to the UI.
func (h *Handler) handlePairGet(c echo.Context) error {
	if h.pairing == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pairing not configured")
	}
	if !loopbackHost(c.Request().Host) {
		return refused("PAIRING_HOST_REFUSED", "Pair from localhost, 127.0.0.1 or [::1].")
	}
	credential, err := h.pairing.Redeem(c.Request().Context(), c.QueryParam("code"))
	if err != nil {
		return err
	}
	http.SetCookie(c.Response(), &http.Cookie{
		Name: PersonCookie, Value: credential, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: c.Request().TLS != nil, MaxAge: pairCookieMaxAge,
	})
	return c.Redirect(http.StatusSeeOther, "/")
}

// handlePairPost redeems a pairing code for a client that is not a browser, and
// answers the credential for it to keep and send as PersonHeader.
func (h *Handler) handlePairPost(c echo.Context) error {
	if h.pairing == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pairing not configured")
	}
	if !loopbackHost(c.Request().Host) {
		return refused("PAIRING_HOST_REFUSED", "Pair from localhost, 127.0.0.1 or [::1].")
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	credential, err := h.pairing.Redeem(c.Request().Context(), in.Code)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"credential": credential})
}

// handleNewPairingCode issues another pairing code. It is person-only itself.
func (h *Handler) handleNewPairingCode(c echo.Context) error {
	if h.pairing == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "pairing not configured")
	}
	code := h.pairing.NewCode()
	return c.JSON(http.StatusOK, map[string]string{"code": code, "path": PairPath + "?code=" + code})
}

// personGate applies personOnly to every route classified as person-only, so a
// route's class is its enforcement: there is no second list to forget. It fails
// closed: a route with no class is treated as person-only until someone
// classifies it (TestEveryRouteIsClassified names it).
func personGate(o options) echo.MiddlewareFunc {
	check := personOnly(o)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		guarded := check(next)
		return func(c echo.Context) error {
			if class, ok := routeClasses[c.Request().Method+" "+c.Path()]; !ok || class == classPerson {
				return guarded(c)
			}
			return next(c)
		}
	}
}
