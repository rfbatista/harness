package httpapi

import (
	"context"
	"net/http"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeFiles answers err, or an empty answer, and keeps what it was asked.
type fakeFiles struct {
	err     error
	session string
	q       ports.SessionEntriesQuery
	root    string
	path    string
	called  string
}

func (f *fakeFiles) SessionRoots(_ context.Context, id string) (*ports.SessionRoots, error) {
	f.called, f.session = "roots", id
	return &ports.SessionRoots{SessionID: id, AgentState: ports.AgentIdle, Roots: []ports.SessionRoot{{Root: "worktree", Label: "Worktree", Available: true}}}, f.err
}

func (f *fakeFiles) SessionEntries(_ context.Context, id string, q ports.SessionEntriesQuery) (*ports.SessionEntries, error) {
	f.called, f.session, f.q = "entries", id, q
	return &ports.SessionEntries{Root: q.Root, Dir: q.Dir, Entries: []ports.SessionEntry{}}, f.err
}

func (f *fakeFiles) SessionFile(_ context.Context, id, root, path string) (*domain.CodeFile, error) {
	f.called, f.session, f.root, f.path = "file", id, root, path
	return &domain.CodeFile{Path: path}, f.err
}

func TestHTTP_SessionFiles_Routes(t *testing.T) {
	f := &fakeFiles{}
	h := NewHandler(Services{SessionFiles: f})
	if code, body := doJSON(t, h, http.MethodGet, "/api/sessions/s1/files", ""); code != http.StatusOK || f.session != "s1" || body["agent_state"] != "idle" {
		t.Errorf("roots: %d %v", code, body)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/sessions/s1/files/entries?root=resources&dir=a%2Fb&after=x&limit=7", ""); code != http.StatusOK ||
		f.q != (ports.SessionEntriesQuery{Root: "resources", Dir: "a/b", After: "x", Limit: 7}) {
		t.Errorf("entries: %d %+v", code, f.q)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/sessions/s1/files/file?root=worktree&path=a+b.go", ""); code != http.StatusOK || f.root != "worktree" || f.path != "a b.go" {
		t.Errorf("file: %d %q %q", code, f.root, f.path)
	}
	f.called = ""
	for _, target := range []string{"/api/sessions/s1/files/entries?root=worktree&limit=many", "/api/sessions/s1/files/entries?root=worktree&limit=-1"} {
		if code, _ := doJSON(t, h, http.MethodGet, target, ""); code != http.StatusBadRequest || f.called != "" {
			t.Errorf("%s: %d, want 400 with no read", target, code)
		}
	}
}

func TestHTTP_SessionFiles_ErrorStatuses(t *testing.T) {
	for code, status := range map[string]int{
		"SESSION_NOT_FOUND": 404, "PATH_NOT_FOUND": 404, "INVALID_INPUT": 400, "FILES_NOT_AVAILABLE": 409, "ROOT_GONE": 410,
	} {
		h := NewHandler(Services{SessionFiles: &fakeFiles{err: &domain.StructuredError{Code: code, Message: "nope"}}})
		for _, route := range []string{"", "/entries?root=worktree", "/file?root=worktree&path=a"} {
			got, body := doJSON(t, h, http.MethodGet, "/api/sessions/s1/files"+route, "")
			if got != status || body["code"] != code || body["error"] != "nope" {
				t.Errorf("%q with %s = %d %v, want %d", route, code, got, body, status)
			}
		}
	}
}

func TestHTTP_SessionFiles_NotConfigured(t *testing.T) {
	h := NewHandler(Services{})
	if code, body := doJSON(t, h, http.MethodGet, "/api/sessions/s1/files", ""); code != http.StatusServiceUnavailable || body["code"] != "UNAVAILABLE" {
		t.Errorf("= %d %v", code, body)
	}
}
