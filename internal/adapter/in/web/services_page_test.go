package web

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeServices serves a project's services to the page's first paint and to
// its re-reads (GET /api/list_services).
type fakeServices struct {
	mu    sync.Mutex
	list  ports.ServiceList
	reads int
}

func (f *fakeServices) ListServices(_ context.Context, _ string) (*ports.ServiceList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.list
	return &l, nil
}

func (f *fakeServices) set(l ports.ServiceList) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list = l
}

func (f *fakeServices) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

func (f *fakeServices) handleList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reads++
	f.mu.Unlock()
	l, _ := f.ListServices(r.Context(), "")
	writeJSON(w, l)
}

func svc(id, name string, status domain.ServiceStatus, run *domain.ServiceRun) *domain.Service {
	return &domain.Service{
		ID: id, ProjectID: "p1", Name: name, Kind: domain.ServiceKindCommand, Status: status, Run: run,
		Command: &domain.ServiceCommand{Line: "make " + name, Ports: []int{8080}},
	}
}

// The Services page: the seeded list, grouped Running and Not running, then
// the services feed moves a row, a gap in the feed re-reads the list, and
// the page keeps what the server says.
func TestServicesPageListsServicesAndFollowsTheFeed(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	started := time.Now().Add(-14 * time.Minute).UTC().Format(time.RFC3339)
	run := &domain.ServiceRun{ID: "r1", StartedAt: started, StartedBy: domain.ServiceStartedBy{Kind: "person"}, Where: domain.ServiceWhere{Kind: "main", Branch: "main"}, Sandboxed: true}
	fake := &fakeServices{list: ports.ServiceList{
		Services: []*domain.Service{svc("s1", "api", domain.ServiceRunning, run), svc("s2", "db", domain.ServiceStopped, nil)},
		Seq:      1, Runtime: ports.ServiceRuntimeInfo{Name: "docker", Available: true}, Sandbox: ports.ServiceSandboxOn,
	}}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:    fakeTickets{},
		Sessions: fakeSessions{},
		Services: fake,
	}, assets, nil)
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/services/events", feed)
	mux.HandleFunc("/api/list_services", fake.handleList)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	rows := `[...document.querySelectorAll('[data-service-group]')].map(g => g.dataset.group + ':' + [...g.querySelectorAll('[data-service]')].map(r => r.dataset.name + '=' + r.querySelector('[data-service-status]').textContent).join(',')).join(' | ')`
	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var got string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/services"),
		poll(`!!document.querySelector('[data-service]')`),
		chromedp.Evaluate(rows, &got),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if want := "running:api=running | stopped:db=stopped"; got != want {
		t.Fatalf("the seeded list = %q, want %q", got, want)
	}

	var badge string
	if err := chromedp.Run(ctx,
		poll(`document.querySelector('[data-running-badge]').textContent === '1 running' && !document.querySelector('[data-running-badge]').hidden`),
		chromedp.Evaluate(`document.querySelector('[data-service][data-name=api] [data-service-port]').href`, &badge),
	); err != nil {
		t.Fatalf("running badge or port link: %v\nJS errors: %v", err, errs.all())
	}
	if badge != "http://localhost:8080/" {
		t.Errorf("port link = %q", badge)
	}

	// The feed's next event moves db into Running.
	waitFollowers(t, feed)
	dbRunning := svc("s2", "db", domain.ServiceRunning, run)
	feed.pushRaw(t, ports.ServiceEvent{Seq: 2, Kind: ports.ServiceChanged, Service: dbRunning, RunningCount: 2})
	if err := chromedp.Run(ctx, poll("("+rows+") === 'running:api=running,db=running'")); err != nil {
		t.Fatalf("db did not move to Running: %v\nJS errors: %v", err, errs.all())
	}

	// An event that skips ahead is a gap: the page reads the list again.
	before := fake.readCount()
	fake.set(ports.ServiceList{
		Services: []*domain.Service{svc("s1", "api", domain.ServiceStopped, nil), svc("s2", "db", domain.ServiceStopped, nil)},
		Seq:      9, Runtime: ports.ServiceRuntimeInfo{Name: "docker", Available: true}, Sandbox: ports.ServiceSandboxOn,
	})
	feed.pushRaw(t, ports.ServiceEvent{Seq: 7, Kind: ports.ServiceChanged, Service: dbRunning, RunningCount: 2})
	if err := chromedp.Run(ctx, poll("("+rows+") === 'stopped:api=stopped,db=stopped'")); err != nil {
		t.Fatalf("the gap did not re-read the list: %v\nJS errors: %v", err, errs.all())
	}
	if fake.readCount() <= before {
		t.Errorf("list_services reads = %d, want more than %d", fake.readCount(), before)
	}
}

func waitFollowers(t *testing.T, f *sseFeed) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.followers() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the page never followed the services feed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
