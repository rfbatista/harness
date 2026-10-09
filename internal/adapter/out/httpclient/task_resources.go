package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TaskResources = (*TaskResources)(nil)
	_ ports.TaskComments  = (*TaskComments)(nil)
)

// TaskResources is ports.TaskResources over the HTTP API.
type TaskResources struct{ c *Client }

// NewTaskResources returns the resources adapter over c.
func NewTaskResources(c *Client) *TaskResources { return &TaskResources{c: c} }

type resourceReply struct {
	Resource *domain.TicketResource `json:"resource"`
}

func (a *TaskResources) ListTicketResources(ctx context.Context, ticketID string) ([]*domain.TicketResource, error) {
	var out struct {
		Resources []*domain.TicketResource `json:"resources"`
	}
	err := a.c.get(ctx, "/api/list_ticket_resources", url.Values{"ticket_id": {ticketID}}, &out)
	return out.Resources, err
}

// UploadTicketResource streams the file as multipart, ticket_id and title
// first, as the server requires.
func (a *TaskResources) UploadTicketResource(ctx context.Context, up ports.ResourceUpload) (*domain.TicketResource, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(writeUpload(mw, up))
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.c.base+"/api/upload_ticket_resource", pr)
	if err != nil {
		pr.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var out resourceReply
	err = a.c.do(req, &out)
	pr.Close() // the server may answer before reading it all
	return out.Resource, err
}

func writeUpload(mw *multipart.Writer, up ports.ResourceUpload) error {
	if err := mw.WriteField("ticket_id", up.TicketID); err != nil {
		return err
	}
	if up.Title != "" {
		if err := mw.WriteField("title", up.Title); err != nil {
			return err
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, up.FileName))
	if up.Mime != "" {
		h.Set("Content-Type", up.Mime)
	}
	w, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if up.Content != nil {
		if _, err := io.Copy(w, up.Content); err != nil {
			return err
		}
	}
	return mw.Close()
}

func (a *TaskResources) AddTicketLink(ctx context.Context, ticketID, link, title string) (*domain.TicketResource, error) {
	var out resourceReply
	err := a.c.post(ctx, "/api/add_ticket_link", map[string]string{"ticket_id": ticketID, "url": link, "title": title}, &out)
	return out.Resource, err
}

func (a *TaskResources) RetitleTicketResource(ctx context.Context, id, title string) (*domain.TicketResource, error) {
	var out resourceReply
	err := a.c.post(ctx, "/api/update_ticket_resource", map[string]string{"resource_id": id, "title": title}, &out)
	return out.Resource, err
}

func (a *TaskResources) DeleteTicketResource(ctx context.Context, id string) error {
	return a.c.post(ctx, "/api/delete_ticket_resource", map[string]string{"resource_id": id}, nil)
}

// OpenTicketResource fetches an upload's bytes. Over the wire only the id,
// MIME type and size of the record are known.
func (a *TaskResources) OpenTicketResource(ctx context.Context, id string) (*ports.ResourceContent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.c.base+"/api/ticket_resources/"+url.PathEscape(id)+"/content", nil)
	if err != nil {
		return nil, err
	}
	if a.c.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.c.token)
	}
	resp, err := a.c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coding_pool server at %s: %w", a.c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, decodeError(resp.StatusCode, body)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, domain.MaxResourceBytes+1))
	if err != nil {
		return nil, err
	}
	modTime, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	return &ports.ResourceContent{
		Resource: &domain.TicketResource{ID: id, Mime: resp.Header.Get("Content-Type"), SizeBytes: int64(len(b))},
		Content:  nopSeekCloser{bytes.NewReader(b)},
		ModTime:  modTime,
	}, nil
}

type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

// TaskComments is ports.TaskComments over the HTTP API.
type TaskComments struct{ c *Client }

// NewTaskComments returns the comments adapter over c.
func NewTaskComments(c *Client) *TaskComments { return &TaskComments{c: c} }

type commentReply struct {
	Comment *domain.TicketComment `json:"comment"`
}

// ListTicketComments narrows by since on this side: the route lists them all.
func (a *TaskComments) ListTicketComments(ctx context.Context, ticketID string, since time.Time) ([]*domain.TicketComment, error) {
	var out struct {
		Comments []*domain.TicketComment `json:"comments"`
	}
	if err := a.c.get(ctx, "/api/list_ticket_comments", url.Values{"ticket_id": {ticketID}}, &out); err != nil {
		return nil, err
	}
	if since.IsZero() {
		return out.Comments, nil
	}
	var after []*domain.TicketComment
	for _, c := range out.Comments {
		if c.CreatedAt.Truncate(time.Millisecond).After(since.Truncate(time.Millisecond)) {
			after = append(after, c)
		}
	}
	return after, nil
}

func (a *TaskComments) AddTicketComment(ctx context.Context, ticketID, body string) (*domain.TicketComment, error) {
	var out commentReply
	err := a.c.post(ctx, "/api/add_ticket_comment", map[string]string{"ticket_id": ticketID, "body": body}, &out)
	return out.Comment, err
}

func (a *TaskComments) UpdateTicketComment(ctx context.Context, id, body string) (*domain.TicketComment, error) {
	var out commentReply
	err := a.c.post(ctx, "/api/update_ticket_comment", map[string]string{"comment_id": id, "body": body}, &out)
	return out.Comment, err
}

func (a *TaskComments) DeleteTicketComment(ctx context.Context, id string) error {
	return a.c.post(ctx, "/api/delete_ticket_comment", map[string]string{"comment_id": id}, nil)
}
