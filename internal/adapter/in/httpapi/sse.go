package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// sseConn is one server-sent event response. Each write must be taken by the
// client within timeout, or it fails: a client that stops reading (a dead or
// stuck one) does not hold its handler, and the feed behind it, forever
// (contract guarantee 6). The handler returns on a failed write, which closes
// the connection and ends what it followed.
type sseConn struct {
	w       *echo.Response
	rc      *http.ResponseController
	timeout time.Duration
}

// openSSE sends an event stream's headers.
func openSSE(c echo.Context, timeout time.Duration) (*sseConn, error) {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	// The controller reaches the connection under echo's writer, whose Flush
	// would swallow the error of a failed flush.
	s := &sseConn{w: w, rc: http.NewResponseController(w.Writer), timeout: timeout}
	return s, s.write(nil)
}

// write sends event (and whatever is buffered before it), failing if the
// client does not take it within the timeout. No lock may be held while it
// runs.
func (s *sseConn) write(event []byte) error {
	if err := s.rc.SetWriteDeadline(time.Now().Add(s.timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if len(event) > 0 {
		if _, err := s.w.Write(event); err != nil {
			return err
		}
	}
	if err := s.rc.Flush(); err != nil {
		return err
	}
	if err := s.rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// ping keeps an idle connection alive.
func (s *sseConn) ping() error {
	return s.write([]byte(": ping\n\n"))
}
