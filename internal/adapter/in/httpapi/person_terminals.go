package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// WithExposedListener says the HTTP server listens beyond this machine. With
// no token, a person's terminal is then refused (ADR 0051, rule 1). The
// caller passes the same test as the startup warning.
func WithExposedListener(exposed bool) Option { return func(o *options) { o.exposed = exposed } }

// personGuard is the one check of ADR 0051, on every route of a person's
// terminal, the WebSocket upgrade included: the server is not open to the
// network without a token, the Host is loopback (DNS rebinding), and an
// Origin, when the request has one, is the harness's own.
func personGuard(o options) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			r := c.Request()
			switch {
			case o.exposed && o.token == "":
				return refused("TERMINAL_SERVER_EXPOSED", "No terminal can open: the harness listens beyond this machine and no API token is set. Set a token or listen on 127.0.0.1.")
			case !loopbackHost(r.Host):
				return refused("TERMINAL_HOST_REFUSED", "No terminal can open from the host name "+r.Host+": reach the harness as localhost, 127.0.0.1 or [::1].")
			case !ownOrigin(r):
				return refused("TERMINAL_ORIGIN_REFUSED", "No terminal can open from another site: the request came from "+r.Header.Get("Origin")+", not from the harness's own page.")
			}
			return next(c)
		}
	}
}

func refused(code, message string) error {
	return &domain.StructuredError{Code: code, Message: message}
}

// loopbackHost says whether a Host header names this machine: localhost,
// 127.0.0.1 or [::1], with any port.
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ownOrigin says whether the request has no Origin (a client that is not a
// browser) or its Origin has the scheme, host and port of the request itself.
func ownOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return u.Scheme == scheme && strings.EqualFold(u.Host, r.Host)
}

// handleOpenPersonTerminal opens the person's shell on a folder of the
// session's virtual file system.
func (h *Handler) handleOpenPersonTerminal(c echo.Context) error {
	if h.persons == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "person terminals not configured")
	}
	var body struct {
		Root string `json:"root"`
		Dir  string `json:"dir"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := bindJSON(c, &body); err != nil {
		return err
	}
	pt, err := h.persons.Open(c.Request().Context(), c.Param("id"), ports.PersonTerminalOpen{
		Root: body.Root, Dir: body.Dir, Size: ports.TermSize{Cols: body.Cols, Rows: body.Rows},
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, pt)
}

// handleListPersonTerminals lists the session's open terminals.
func (h *Handler) handleListPersonTerminals(c echo.Context) error {
	if h.persons == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "person terminals not configured")
	}
	list, err := h.persons.List(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, list)
}

// handlePersonTerminal attaches a client over a WebSocket, with the session
// terminal's protocol. The id is checked before the upgrade.
func (h *Handler) handlePersonTerminal(c echo.Context) error {
	if h.persons == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "person terminals not configured")
	}
	t, err := h.persons.Attach(c.Param("tid"))
	if err != nil {
		return err
	}
	return serveTerminal(c, t)
}

// handleClosePersonTerminal hangs up the shell.
func (h *Handler) handleClosePersonTerminal(c echo.Context) error {
	if h.persons == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "person terminals not configured")
	}
	if err := h.persons.Close(c.Param("tid")); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}
