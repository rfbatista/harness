package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeCode answers err, or empty answers, and keeps what it was asked.
type fakeCode struct {
	err    error
	src    ports.CodeSourceQuery
	path   string
	diff   ports.CodeDiffQuery
	called string
}

func (f *fakeCode) CodeTree(_ context.Context, q ports.CodeSourceQuery, dir string) (*domain.CodeTree, error) {
	f.called, f.src, f.path = "tree", q, dir
	return &domain.CodeTree{Entries: []domain.TreeEntry{}}, f.err
}

func (f *fakeCode) CodePaths(_ context.Context, q ports.CodeSourceQuery) (*domain.CodePaths, error) {
	f.called, f.src = "paths", q
	return &domain.CodePaths{Paths: []string{}}, f.err
}

func (f *fakeCode) CodeFile(_ context.Context, q ports.CodeSourceQuery, p string) (*domain.CodeFile, error) {
	f.called, f.src, f.path = "file", q, p
	return &domain.CodeFile{Zones: []string{}}, f.err
}

func (f *fakeCode) CodeDiff(_ context.Context, q ports.CodeDiffQuery) (*domain.CodeDiff, error) {
	f.called, f.diff = "diff", q
	return &domain.CodeDiff{Files: []domain.FileDiff{}}, f.err
}

func (f *fakeCode) CodeMap(_ context.Context, q ports.CodeSourceQuery) (*domain.CodeMap, error) {
	f.called, f.src = "map", q
	return &domain.CodeMap{}, f.err
}

func TestHTTP_Code_ReadsTheQuery(t *testing.T) {
	f := &fakeCode{}
	h := NewHandler(Services{Code: f})
	if code, _ := doJSON(t, h, http.MethodGet, "/api/code/tree?repository_id=r1&ref=main&path=internal%2Fapp", ""); code != http.StatusOK ||
		f.called != "tree" || f.src != (ports.CodeSourceQuery{RepositoryID: "r1", Ref: "main"}) || f.path != "internal/app" {
		t.Errorf("tree: %d, %+v %q", code, f.src, f.path)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/code/paths?workspace_id=w1", ""); code != http.StatusOK || f.called != "paths" || f.src.WorkspaceID != "w1" {
		t.Errorf("paths: %d, %+v", code, f.src)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/code/file?workspace_id=w1&path=a+b.go", ""); code != http.StatusOK || f.called != "file" || f.path != "a b.go" {
		t.Errorf("file: %d, %q", code, f.path)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/code/map?repository_id=r1", ""); code != http.StatusOK || f.called != "map" {
		t.Errorf("map: %d", code)
	}
	if code, _ := doJSON(t, h, http.MethodGet, "/api/code/diff?repository_id=r1&base=main&head=feature&path=x.go&scope=s&commit=c&workspace_id=w", ""); code != http.StatusOK ||
		f.diff != (ports.CodeDiffQuery{RepositoryID: "r1", Base: "main", Head: "feature", Path: "x.go", Scope: "s", Commit: "c", WorkspaceID: "w"}) {
		t.Errorf("diff: %d, %+v", code, f.diff)
	}
}

func TestHTTP_Code_ErrorStatuses(t *testing.T) {
	for code, status := range map[string]int{
		"INVALID_INPUT": 400, "REPOSITORY_NOT_FOUND": 404, "WORKSPACE_NOT_FOUND": 404, "COMMIT_NOT_FOUND": 404,
		"REF_NOT_FOUND": 404, "PATH_NOT_FOUND": 404, "WORKSPACE_MISSING": 409,
	} {
		h := NewHandler(Services{Code: &fakeCode{err: &domain.StructuredError{Code: code, Message: "nope"}}})
		for _, route := range []string{"tree", "paths", "file", "diff", "map"} {
			got, body := doJSON(t, h, http.MethodGet, "/api/code/"+route+"?repository_id=r1", "")
			if got != status || body["code"] != code || body["error"] != "nope" {
				t.Errorf("%s with %s = %d %v, want %d", route, code, got, body, status)
			}
		}
	}
}

func TestHTTP_Code_NotConfigured(t *testing.T) {
	h := NewHandler(Services{})
	for _, route := range []string{"tree", "paths", "file", "diff", "map"} {
		if code, body := doJSON(t, h, http.MethodGet, "/api/code/"+route+"?repository_id=r1", ""); code != http.StatusServiceUnavailable || body["code"] != "UNAVAILABLE" {
			t.Errorf("%s = %d %v", route, code, body)
		}
	}
}

func TestHTTP_Events_StreamsCodeChanged(t *testing.T) {
	ch := make(chan ports.ProjectChange, 1)
	ch <- ports.ProjectChange{CodeChanged: &ports.CodeChange{WorkspaceID: "w1", Head: "abc", ProjectID: "p1"}}
	srv := httptest.NewServer(NewRouter(&Handler{orchSvc: feedOnly{ch: ch}}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events?project_id=p1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		data, ok := strings.CutPrefix(scan.Text(), "data: ")
		if !ok {
			continue
		}
		if data != `{"code_changed":{"workspace_id":"w1","head":"abc"}}` {
			t.Fatalf("event = %s", data)
		}
		return
	}
	t.Fatalf("no event: %v", scan.Err())
}
