package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/blobstore"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func newResourcesHandler(t *testing.T) (*Handler, *planning.Service, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, _ := projects.Create("proj", "/tmp/proj")
	svc := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	svc.Resources, svc.Comments = sqlite.NewTicketResourceRepository(db), sqlite.NewTicketCommentRepository(db)
	svc.Blobs, svc.ContentURL = blobstore.New(t.TempDir()), TicketResourceContentPath
	tk, err := svc.CreateTicket(context.Background(), p.ID, "Task", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{planningSvc: svc, resources: svc, comments: svc}, svc, tk.ID
}

type part struct{ name, fileName, contentType, body string }

func multipartBody(t *testing.T, parts ...part) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disp := `form-data; name="` + p.name + `"`
		if p.fileName != "" {
			disp += `; filename="` + p.fileName + `"`
		}
		h.Set("Content-Disposition", disp)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(pw, p.body)
	}
	w.Close()
	return w.FormDataContentType(), &buf
}

type wireReply struct {
	Resource *domain.TicketResource  `json:"resource"`
	Comment  *domain.TicketComment   `json:"comment"`
	Res      []domain.TicketResource `json:"resources"`
	Comments []domain.TicketComment  `json:"comments"`
	Code     string                  `json:"code"`
}

func serveReq(t *testing.T, h *Handler, req *http.Request) (*httptest.ResponseRecorder, wireReply) {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(h).ServeHTTP(rec, req)
	var out wireReply
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func upload(t *testing.T, h *Handler, parts ...part) (*httptest.ResponseRecorder, wireReply) {
	t.Helper()
	ct, body := multipartBody(t, parts...)
	req := httptest.NewRequest(http.MethodPost, "/api/upload_ticket_resource", body)
	req.Header.Set("Content-Type", ct)
	return serveReq(t, h, req)
}

func postWire(t *testing.T, h *Handler, path string, body any) (*httptest.ResponseRecorder, wireReply) {
	t.Helper()
	b, _ := json.Marshal(body)
	return serveReq(t, h, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
}

func TestHTTP_UploadThenServeContent(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	rec, out := upload(t, h, part{name: "ticket_id", body: tid}, part{name: "title", body: "Login"},
		part{name: "file", fileName: "shot.png", contentType: "image/png", body: "\x89PNG\r\n\x1a\nrest"})
	if rec.Code != 200 || out.Resource == nil {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	r := out.Resource
	if r.Title != "Login" || r.Kind != domain.ResourceImage || r.SizeBytes != 12 || r.ContentURL != "/api/ticket_resources/"+r.ID+"/content" {
		t.Fatalf("resource = %+v", r)
	}

	rec, _ = serveReq(t, h, httptest.NewRequest(http.MethodGet, r.ContentURL, nil))
	if rec.Code != 200 || rec.Body.String() != "\x89PNG\r\n\x1a\nrest" {
		t.Fatalf("content = %d %q", rec.Code, rec.Body)
	}
	hdr := rec.Header()
	if hdr.Get("Content-Type") != "image/png" || hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("headers = %v", hdr)
	}
	if d := hdr.Get("Content-Disposition"); !strings.HasPrefix(d, "inline;") || !strings.Contains(d, "filename*=UTF-8''Login") {
		t.Fatalf("disposition = %q", d)
	}

	rec, _ = serveReq(t, h, httptest.NewRequest(http.MethodGet, r.ContentURL+"?download=1", nil))
	if d := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(d, "attachment;") {
		t.Fatalf("download disposition = %q", d)
	}

	req := httptest.NewRequest(http.MethodGet, r.ContentURL, nil)
	req.Header.Set("Range", "bytes=1-3")
	rec, _ = serveReq(t, h, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "PNG" {
		t.Fatalf("range = %d %q", rec.Code, rec.Body)
	}
	rec, _ = serveReq(t, h, httptest.NewRequest(http.MethodHead, r.ContentURL, nil))
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "12" {
		t.Fatalf("head = %d len %s body %q", rec.Code, rec.Header().Get("Content-Length"), rec.Body)
	}

	rec, out = serveReq(t, h, httptest.NewRequest(http.MethodGet, "/api/list_ticket_resources?ticket_id="+tid, nil))
	if rec.Code != 200 || len(out.Res) != 1 || out.Res[0].ID != r.ID {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}

// Only images (not SVG), audio, PDF and plain text are shown inline;
// everything else downloads. Text gets a charset when it had none.
func TestHTTP_ContentDispositionByType(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	cases := []struct {
		mime, wantType, wantDisp string
	}{
		{"image/jpeg", "image/jpeg", "inline"},
		{"image/svg+xml", "image/svg+xml", "attachment"},
		{"audio/ogg", "audio/ogg", "inline"},
		{"application/pdf", "application/pdf", "inline"},
		{"text/plain", "text/plain; charset=utf-8", "inline"},
		{"text/plain; charset=iso-8859-1", "text/plain; charset=iso-8859-1", "inline"},
		{"text/html", "text/html", "attachment"},
		{"application/javascript", "application/javascript", "attachment"},
		{"application/zip", "application/zip", "attachment"},
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			rec, out := upload(t, h, part{name: "ticket_id", body: tid}, part{name: "file", fileName: "x", contentType: c.mime, body: "<b>x</b>"})
			if rec.Code != 200 {
				t.Fatalf("upload = %d %s", rec.Code, rec.Body)
			}
			rec, _ = serveReq(t, h, httptest.NewRequest(http.MethodGet, out.Resource.ContentURL, nil))
			if got := rec.Header().Get("Content-Type"); got != c.wantType {
				t.Fatalf("content-type = %q, want %q", got, c.wantType)
			}
			if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, c.wantDisp+";") {
				t.Fatalf("disposition = %q, want %s", got, c.wantDisp)
			}
			if rec.Header().Get("Content-Security-Policy") != "sandbox" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("headers = %v", rec.Header())
			}
		})
	}
}

func TestHTTP_ContentDispositionEncodesTheTitle(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	_, out := upload(t, h, part{name: "ticket_id", body: tid}, part{name: "title", body: `Café "plan"; v2.txt`},
		part{name: "file", fileName: "x.txt", contentType: "text/plain", body: "x"})
	rec, _ := serveReq(t, h, httptest.NewRequest(http.MethodGet, out.Resource.ContentURL, nil))
	d := rec.Header().Get("Content-Disposition")
	if !strings.Contains(d, `filename*=UTF-8''Caf%C3%A9%20%22plan%22%3B%20v2.txt`) || strings.Count(d, ";") != 2 {
		t.Fatalf("disposition = %q", d)
	}
}

func TestHTTP_UploadRefusals(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	file := part{name: "file", fileName: "a.txt", contentType: "text/plain", body: "a"}
	cases := []struct {
		name   string
		parts  []part
		status int
		code   string
	}{
		{"file before ticket_id", []part{file, {name: "ticket_id", body: tid}}, 400, "INVALID_INPUT"},
		{"no file", []part{{name: "ticket_id", body: tid}}, 400, "INVALID_INPUT"},
		{"no such task", []part{{name: "ticket_id", body: "missing"}, file}, 404, "TICKET_NOT_FOUND"},
		{"empty file", []part{{name: "ticket_id", body: tid}, {name: "file", fileName: "e", body: ""}}, 400, "INVALID_INPUT"},
		{"blank-only title is the file name", []part{{name: "ticket_id", body: tid}, {name: "title", body: "  "}, file}, 200, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, out := upload(t, h, c.parts...)
			if rec.Code != c.status || out.Code != c.code {
				t.Fatalf("= %d %q (%s), want %d %s", rec.Code, out.Code, rec.Body, c.status, c.code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/upload_ticket_resource", strings.NewReader(`{"ticket_id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	if rec, out := serveReq(t, h, req); rec.Code != 400 || out.Code != "INVALID_INPUT" {
		t.Fatalf("not multipart = %d %s", rec.Code, rec.Body)
	}
}

// countingReader yields n bytes of 'x' and counts what was read.
type countingReader struct{ left, read int }

func (r *countingReader) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.left)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.left -= n
	r.read += n
	return n, nil
}

// Over the cap is 413 RESOURCE_TOO_LARGE, answered without reading the rest
// of the body.
func TestHTTP_UploadOverTheCapStopsEarly(t *testing.T) {
	h, svc, tid := newResourcesHandler(t)
	svc.MaxResourceBytes = 1 << 10
	ct, head := multipartBody(t, part{name: "ticket_id", body: tid})
	// Drop the closing boundary and stream a huge file part after it.
	boundary := strings.TrimPrefix(ct, "multipart/form-data; boundary=")
	prefix := strings.TrimSuffix(head.String(), "--"+boundary+"--\r\n")
	prefix += "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n\r\n"
	big := &countingReader{left: 64 << 20}
	req := httptest.NewRequest(http.MethodPost, "/api/upload_ticket_resource", io.MultiReader(strings.NewReader(prefix), big))
	req.Header.Set("Content-Type", ct)
	rec, out := serveReq(t, h, req)
	if rec.Code != http.StatusRequestEntityTooLarge || out.Code != "RESOURCE_TOO_LARGE" {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
	if big.read > 1<<20 {
		t.Fatalf("read %d bytes of the body before refusing", big.read)
	}
}

// A body past the transport limit is refused with the same code, even when
// the service would take more.
func TestHTTP_UploadBodyLimit(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	ct, head := multipartBody(t, part{name: "ticket_id", body: tid})
	boundary := strings.TrimPrefix(ct, "multipart/form-data; boundary=")
	prefix := strings.TrimSuffix(head.String(), "--"+boundary+"--\r\n")
	prefix += "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n\r\n"
	big := &countingReader{left: int(domain.MaxResourceBytes) + uploadAllowance + 1}
	req := httptest.NewRequest(http.MethodPost, "/api/upload_ticket_resource", io.MultiReader(strings.NewReader(prefix), big))
	req.Header.Set("Content-Type", ct)
	if rec, out := serveReq(t, h, req); rec.Code != http.StatusRequestEntityTooLarge || out.Code != "RESOURCE_TOO_LARGE" {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
}

func TestHTTP_LinksRetitleDelete(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	rec, out := postWire(t, h, "/api/add_ticket_link", map[string]string{"ticket_id": tid, "url": "https://example.com", "title": "Ex"})
	if rec.Code != 200 || out.Resource.Kind != domain.ResourceLink || out.Resource.ContentURL != "" {
		t.Fatalf("add link = %d %s", rec.Code, rec.Body)
	}
	id := out.Resource.ID
	if rec, out := postWire(t, h, "/api/update_ticket_resource", map[string]string{"resource_id": id, "title": "Example"}); rec.Code != 200 || out.Resource.Title != "Example" {
		t.Fatalf("retitle = %d %s", rec.Code, rec.Body)
	}
	if rec, _ := serveReq(t, h, httptest.NewRequest(http.MethodGet, "/api/ticket_resources/"+id+"/content", nil)); rec.Code != 404 {
		t.Fatalf("content of a link = %d", rec.Code)
	}
	for _, c := range []struct {
		name, path string
		body       map[string]string
		status     int
		code       string
	}{
		{"link not http", "/api/add_ticket_link", map[string]string{"ticket_id": tid, "url": "file:///etc/passwd"}, 400, "INVALID_INPUT"},
		{"link to a missing task", "/api/add_ticket_link", map[string]string{"ticket_id": "missing", "url": "https://x.io"}, 404, "TICKET_NOT_FOUND"},
		{"blank title", "/api/update_ticket_resource", map[string]string{"resource_id": id, "title": " "}, 400, "INVALID_INPUT"},
		{"retitle missing", "/api/update_ticket_resource", map[string]string{"resource_id": "missing", "title": "x"}, 404, "RESOURCE_NOT_FOUND"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec, out := postWire(t, h, c.path, c.body); rec.Code != c.status || out.Code != c.code {
				t.Fatalf("= %d %s", rec.Code, rec.Body)
			}
		})
	}
	if rec, _ := postWire(t, h, "/api/delete_ticket_resource", map[string]string{"resource_id": id}); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if rec, out := postWire(t, h, "/api/delete_ticket_resource", map[string]string{"resource_id": id}); rec.Code != 404 || out.Code != "RESOURCE_NOT_FOUND" {
		t.Fatalf("delete again = %d %s", rec.Code, rec.Body)
	}
	if rec, out := serveReq(t, h, httptest.NewRequest(http.MethodGet, "/api/list_ticket_resources?ticket_id=missing", nil)); rec.Code != 404 || out.Code != "TICKET_NOT_FOUND" {
		t.Fatalf("list missing = %d %s", rec.Code, rec.Body)
	}
}

func TestHTTP_Comments(t *testing.T) {
	h, _, tid := newResourcesHandler(t)
	rec, out := postWire(t, h, "/api/add_ticket_comment", map[string]string{"ticket_id": tid, "body": " <b>hi</b>\nthere "})
	if rec.Code != 200 || out.Comment.Body != "<b>hi</b>\nthere" || out.Comment.Author != domain.CommentByPerson {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	id := out.Comment.ID
	if rec, out := postWire(t, h, "/api/update_ticket_comment", map[string]string{"comment_id": id, "body": "edited"}); rec.Code != 200 || out.Comment.Body != "edited" {
		t.Fatalf("update = %d %s", rec.Code, rec.Body)
	}
	if rec, out := serveReq(t, h, httptest.NewRequest(http.MethodGet, "/api/list_ticket_comments?ticket_id="+tid, nil)); rec.Code != 200 || len(out.Comments) != 1 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		name, path string
		body       map[string]string
		status     int
		code       string
	}{
		{"empty", "/api/add_ticket_comment", map[string]string{"ticket_id": tid, "body": "  "}, 400, "INVALID_INPUT"},
		{"missing task", "/api/add_ticket_comment", map[string]string{"ticket_id": "missing", "body": "x"}, 404, "TICKET_NOT_FOUND"},
		{"update missing", "/api/update_ticket_comment", map[string]string{"comment_id": "missing", "body": "x"}, 404, "COMMENT_NOT_FOUND"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec, out := postWire(t, h, c.path, c.body); rec.Code != c.status || out.Code != c.code {
				t.Fatalf("= %d %s", rec.Code, rec.Body)
			}
		})
	}
	if rec, _ := postWire(t, h, "/api/delete_ticket_comment", map[string]string{"comment_id": id}); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if rec, out := postWire(t, h, "/api/delete_ticket_comment", map[string]string{"comment_id": id}); rec.Code != 404 || out.Code != "COMMENT_NOT_FOUND" {
		t.Fatalf("delete again = %d %s", rec.Code, rec.Body)
	}
}

func TestHTTP_ResourcesUnconfiguredAnswer503(t *testing.T) {
	h := &Handler{}
	for _, path := range []string{"/api/list_ticket_resources?ticket_id=t", "/api/list_ticket_comments?ticket_id=t", "/api/ticket_resources/x/content"} {
		if rec, _ := serveReq(t, h, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != 503 {
			t.Fatalf("%s = %d", path, rec.Code)
		}
	}
}

// The project feed carries resource and comment changes under their own
// keys, deletions as ids only.
func TestHTTP_Events_StreamsResourceAndCommentChanges(t *testing.T) {
	ch := make(chan ports.ProjectChange, 2)
	h := &Handler{orchSvc: feedOnly{ch: ch}}
	r := &domain.TicketResource{ID: "r1", TicketID: "tk1", ProjectID: "p1", Kind: domain.ResourceLink, Title: "X", URL: "https://x.io"}
	ch <- ports.TicketResourceUpdated(r)
	ch <- ports.TicketCommentRemoved(&domain.TicketComment{ID: "c1", TicketID: "tk1", ProjectID: "p1", Body: "secret"})

	srv := httptest.NewServer(NewRouter(h))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events?project_id=p1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var lines []string
	scan := bufio.NewScanner(resp.Body)
	for len(lines) < 2 && scan.Scan() {
		if data, ok := strings.CutPrefix(scan.Text(), "data: "); ok {
			lines = append(lines, data)
		}
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[0], `{"ticket_resource":{"id":"r1"`) ||
		lines[1] != `{"ticket_comment":{"id":"c1","ticket_id":"tk1","project_id":"p1"},"deleted":true}` {
		t.Fatalf("lines = %v", lines)
	}
}
