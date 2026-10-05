package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Option configures the router.
type Option func(*options)

type options struct{ token string }

// WithToken requires `Authorization: Bearer <token>` on every /api route but
// the open ones: /api/health, so a client or a probe can tell the server is
// up, and the SessionStart hook route, which is loopback-only and called by
// claude itself. Without it, or with an empty token, the API is open.
func WithToken(token string) Option { return func(o *options) { o.token = token } }

// openRoutes answer without a token.
var openRoutes = map[string]bool{
	"/api/health":                 true,
	InteractiveSessionStartedPath: true,
}

func requireToken(token string) echo.MiddlewareFunc {
	want := []byte("Bearer " + token)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if openRoutes[c.Request().URL.Path] {
				return next(c)
			}
			got := []byte(strings.TrimSpace(c.Request().Header.Get("Authorization")))
			if subtle.ConstantTimeCompare(got, want) != 1 {
				return c.JSON(http.StatusUnauthorized, errorBody("a valid bearer token is required", "UNAUTHORIZED"))
			}
			return next(c)
		}
	}
}
