package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeThreads reads product-owner threads by id.
type fakeThreads []domain.ProductOwnerThread

func (f fakeThreads) Thread(_ context.Context, id, _ string) (*ports.ProductOwnerThreadPage, error) {
	for _, t := range f {
		if t.ID == id {
			return &ports.ProductOwnerThreadPage{Thread: t}, nil
		}
	}
	return nil, errs.Newf("THREAD_NOT_FOUND", "no thread %s", id)
}

// A product-owner thread has a Context page that mounts the Context island
// for the thread's session, and no session list shows the session (ADR 0109).
func TestAProductOwnerThreadsContextPageMountsTheContextIslandForItsSession(t *testing.T) {
	h := newTestHandler(t, fragmentWorld(t))
	rec := get(t, h, "/projects/p1/product-owner/threads/th-1/context")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-island="session-context"`,
		`data-island-props="session-context">{"session_id":"s-po","project_id":"p1","live":true,"group":"","parent":"","focus":false,"whiteboard_href":""}</script>`,
		"What was done", `href="/projects/p1/product-owner/threads/th-1"`, "<h1>Context</h1>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestAThreadOfAnotherProjectOrAnUnknownOneHasNoContextPage(t *testing.T) {
	h := newTestHandler(t, fragmentWorld(t))
	for name, path := range map[string]string{
		"a thread of another project": "/projects/p1/product-owner/threads/th-2/context",
		"an unknown thread":           "/projects/p1/product-owner/threads/nope/context",
		"an unknown project":          "/projects/nope/product-owner/threads/th-1/context",
	} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
	}
}

// The React client serves the thread page and may serve the rest of the product
// owner's pages under a wildcard. The Context page is longer than both, so it
// wins, and registering the three together does not conflict.
func TestTheContextPagePatternWinsOverTheThreadPagesAndAProductOwnerWildcard(t *testing.T) {
	served := ""
	at := func(name string) http.HandlerFunc {
		return func(http.ResponseWriter, *http.Request) { served = name }
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /projects/{project}/product-owner/{path...}", at("wildcard"))
	mux.HandleFunc("GET /projects/{project}/product-owner/threads/{thread}", at("thread"))
	mux.HandleFunc("GET /projects/{project}/product-owner/threads/{thread}/context", at("context"))
	for path, want := range map[string]string{
		"/projects/p1/product-owner/threads/th-1/context": "context",
		"/projects/p1/product-owner/threads/th-1":         "thread",
		"/projects/p1/product-owner/memory":               "wildcard",
	} {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if served != want {
			t.Errorf("%s served by %s, want %s", path, served, want)
		}
	}
}
