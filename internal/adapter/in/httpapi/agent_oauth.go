package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The agent OAuth flow (ADR 0018): `claude setup-token` run for the person.
// Every endpoint answers the flow's state, which carries the authorization
// URL but never the token and never the code that was pasted.

var errAgentOAuthOff = echo.NewHTTPError(http.StatusServiceUnavailable, "agent sign-in not configured")

func (h *Handler) handleGetAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	return c.JSON(http.StatusOK, h.agentOAuth.State())
}

func (h *Handler) handleStartAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	st, err := h.agentOAuth.Start()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleSubmitAgentOAuthCode(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	st, err := h.agentOAuth.Submit(in.Code)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (h *Handler) handleCancelAgentOAuth(c echo.Context) error {
	if h.agentOAuth == nil {
		return errAgentOAuthOff
	}
	return c.JSON(http.StatusOK, h.agentOAuth.Cancel())
}

// agentAuthTimeout bounds one `claude auth status` an endpoint waits for.
const agentAuthTimeout = 10 * time.Second

// agentAuthAnswer is what the page learns of the login: the account fields
// claude's status prints and whether the status could be read. Never a token,
// never the login file's content.
type agentAuthAnswer struct {
	SignedIn     bool   `json:"signed_in"`
	Account      string `json:"account"`
	Subscription string `json:"subscription"`
	AuthMethod   string `json:"auth_method"`
	CheckedOK    bool   `json:"checked_ok"`
}

// authAnswer asks `claude auth status`; a status that cannot be read is
// signed out and not checked.
func (h *Handler) authAnswer(c echo.Context) agentAuthAnswer {
	ctx, cancel := context.WithTimeout(c.Request().Context(), agentAuthTimeout)
	defer cancel()
	st, err := h.agentAuth.Status(ctx)
	if err != nil {
		return agentAuthAnswer{}
	}
	return agentAuthAnswer{SignedIn: st.LoggedIn, Account: st.Email, Subscription: st.SubscriptionType, AuthMethod: st.AuthMethod, CheckedOK: true}
}

func (h *Handler) handleGetAgentAuthStatus(c echo.Context) error {
	if h.agentAuth == nil {
		return errAgentOAuthOff
	}
	return c.JSON(http.StatusOK, h.authAnswer(c))
}

// handleSignOutAgentAuth runs `claude auth logout` and answers the status
// read afterwards.
func (h *Handler) handleSignOutAgentAuth(c echo.Context) error {
	if h.agentAuth == nil {
		return errAgentOAuthOff
	}
	if err := h.agentAuth.Logout(c.Request().Context()); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, h.authAnswer(c))
}
