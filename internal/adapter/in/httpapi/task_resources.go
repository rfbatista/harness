package httpapi

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The resources and comments a person adds to a task (see "Contract: Harness
// server ↔ Web UI — task resources and comments").

// TicketResourceContentPath is where an upload's bytes are served. Planning
// is given this function to fill content_url, rather than importing the
// adapter to build the route.
func TicketResourceContentPath(id string) string {
	return "/api/ticket_resources/" + url.PathEscape(id) + "/content"
}

// ResourceCSP is the policy uploaded content is served under: a sandbox
// with nothing allowed, so nothing a person uploads runs on the harness
// origin, even opened directly.
const ResourceCSP = "sandbox"

// uploadAllowance is how far past the file cap a request body may run, for
// the multipart framing and the small fields before the file.
const uploadAllowance = 64 << 10

// maxFieldBytes caps one non-file field of an upload.
const maxFieldBytes = 4 << 10

var errResourcesOff = echo.NewHTTPError(http.StatusServiceUnavailable, "task resources not configured")

func tooLarge(c echo.Context) error {
	// The rest of the body is never read: tell the client not to reuse the
	// connection for another request.
	c.Response().Header().Set("Connection", "close")
	return &domain.StructuredError{Code: "RESOURCE_TOO_LARGE", Message: "a file is limited to 25 MiB"}
}

func isBodyTooLarge(err error) bool {
	var mb *http.MaxBytesError
	return errors.As(err, &mb)
}

func (h *Handler) handleListTicketResources(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	list, err := h.resources.ListTicketResources(c.Request().Context(), c.QueryParam("ticket_id"))
	if err != nil {
		return err
	}
	if list == nil {
		list = []*domain.TicketResource{}
	}
	return c.JSON(http.StatusOK, map[string]any{"resources": list})
}

// handleUploadTicketResource streams the file part straight into the store:
// the body is never buffered whole. ticket_id (and title) must come before
// the file, so the task is checked before a byte is kept.
func (h *Handler) handleUploadTicketResource(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, domain.MaxResourceBytes+uploadAllowance)
	mr, err := req.MultipartReader()
	if err != nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "the body must be multipart/form-data"}
	}
	var up ports.ResourceUpload
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "file is required"}
		}
		if err != nil {
			if isBodyTooLarge(err) {
				return tooLarge(c)
			}
			return &domain.StructuredError{Code: "INVALID_INPUT", Message: "malformed multipart body"}
		}
		switch part.FormName() {
		case "ticket_id", "title":
			v, err := io.ReadAll(io.LimitReader(part, maxFieldBytes))
			if err != nil {
				if isBodyTooLarge(err) {
					return tooLarge(c)
				}
				return &domain.StructuredError{Code: "INVALID_INPUT", Message: "malformed multipart body"}
			}
			if part.FormName() == "ticket_id" {
				up.TicketID = strings.TrimSpace(string(v))
			} else {
				up.Title = string(v)
			}
		case "file":
			if up.TicketID == "" {
				return &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id (and title) must come before file"}
			}
			up.FileName, up.Mime, up.Content = part.FileName(), part.Header.Get("Content-Type"), part
			r, err := h.resources.UploadTicketResource(req.Context(), up)
			if err != nil {
				if isBodyTooLarge(err) || errorCode(err) == "RESOURCE_TOO_LARGE" {
					return tooLarge(c)
				}
				return err
			}
			return c.JSON(http.StatusOK, map[string]any{"resource": r})
		}
	}
}

func errorCode(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func (h *Handler) handleAddTicketLink(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	var in struct {
		TicketID string `json:"ticket_id"`
		URL      string `json:"url"`
		Title    string `json:"title"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	r, err := h.resources.AddTicketLink(c.Request().Context(), in.TicketID, in.URL, in.Title)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"resource": r})
}

func (h *Handler) handleUpdateTicketResource(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	var in struct {
		ResourceID string `json:"resource_id"`
		Title      string `json:"title"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	r, err := h.resources.RetitleTicketResource(c.Request().Context(), in.ResourceID, in.Title)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"resource": r})
}

func (h *Handler) handleDeleteTicketResource(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	var in struct {
		ResourceID string `json:"resource_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.resources.DeleteTicketResource(c.Request().Context(), in.ResourceID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct{}{})
}

// handleTicketResourceContent serves an upload's bytes. Only types a browser
// shows without running anything go inline; the rest download, and the
// sandbox CSP and nosniff hold either way. http.ServeContent supplies
// Content-Length, Range, conditional requests and HEAD.
func (h *Handler) handleTicketResourceContent(c echo.Context) error {
	if h.resources == nil {
		return errResourcesOff
	}
	rc, err := h.resources.OpenTicketResource(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	defer rc.Content.Close()

	ct := rc.Resource.Mime
	base := domain.MediaType(ct)
	if base == "text/plain" && !strings.Contains(strings.ToLower(ct), "charset=") {
		ct = "text/plain; charset=utf-8"
	}
	disposition := "attachment"
	if servedInline(base) && c.QueryParam("download") != "1" {
		disposition = "inline"
	}
	hdr := c.Response().Header()
	hdr.Set("Content-Type", ct)
	hdr.Set("Content-Disposition", disposition+"; "+filenameParams(rc.Resource.Title))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", ResourceCSP)
	hdr.Set("Cache-Control", "private, no-cache")
	// The headers carry the title, so a retitle must change the validators
	// as much as new bytes would.
	changed := rc.ModTime
	if rc.Resource.UpdatedAt.After(changed) {
		changed = rc.Resource.UpdatedAt
	}
	hdr.Set("ETag", `"`+strconv.FormatInt(rc.ModTime.UnixNano(), 36)+"-"+strconv.FormatInt(rc.Resource.UpdatedAt.UnixMilli(), 36)+`"`)
	http.ServeContent(c.Response(), c.Request(), "", changed.Truncate(time.Second), rc.Content)
	return nil
}

// servedInline says which media types a browser may show in place.
func servedInline(base string) bool {
	switch {
	case base == "image/svg+xml":
		return false
	case strings.HasPrefix(base, "image/"), strings.HasPrefix(base, "audio/"):
		return true
	}
	return base == "application/pdf" || base == "text/plain"
}

// filenameParams names the download after title: an ASCII fallback and the
// exact title in RFC 5987 form.
func filenameParams(title string) string {
	ascii := make([]byte, 0, len(title))
	for _, r := range title {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == ';' {
			r = '_'
		}
		ascii = append(ascii, byte(r))
	}
	return `filename="` + string(ascii) + `"; filename*=UTF-8''` + rfc5987(title)
}

// rfc5987 percent-encodes s, keeping only RFC 5987 attr-chars as they are.
func rfc5987(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.IndexByte("!#$&+-.^_`|~", ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[ch>>4])
		b.WriteByte(hex[ch&15])
	}
	return b.String()
}

// --- Comments ---

func (h *Handler) handleListTicketComments(c echo.Context) error {
	if h.comments == nil {
		return errResourcesOff
	}
	list, err := h.comments.ListTicketComments(c.Request().Context(), c.QueryParam("ticket_id"), time.Time{})
	if err != nil {
		return err
	}
	if list == nil {
		list = []*domain.TicketComment{}
	}
	return c.JSON(http.StatusOK, map[string]any{"comments": list})
}

func (h *Handler) handleAddTicketComment(c echo.Context) error {
	if h.comments == nil {
		return errResourcesOff
	}
	var in struct {
		TicketID string `json:"ticket_id"`
		Body     string `json:"body"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cm, err := h.comments.AddTicketComment(c.Request().Context(), in.TicketID, in.Body)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"comment": cm})
}

func (h *Handler) handleUpdateTicketComment(c echo.Context) error {
	if h.comments == nil {
		return errResourcesOff
	}
	var in struct {
		CommentID string `json:"comment_id"`
		Body      string `json:"body"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cm, err := h.comments.UpdateTicketComment(c.Request().Context(), in.CommentID, in.Body)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"comment": cm})
}

func (h *Handler) handleDeleteTicketComment(c echo.Context) error {
	if h.comments == nil {
		return errResourcesOff
	}
	var in struct {
		CommentID string `json:"comment_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.comments.DeleteTicketComment(c.Request().Context(), in.CommentID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct{}{})
}
