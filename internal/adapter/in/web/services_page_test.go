package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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

// The navigation's Services item shows the running count on every project
// page, as the server rendered it, and the services feed keeps it current.
func TestProjectNavServicesCountFollowsTheFeedOnAnotherPage(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	fake := &fakeServices{list: ports.ServiceList{Services: []*domain.Service{}, Seq: 1, Runtime: ports.ServiceRuntimeInfo{Name: "docker", Available: true}, Sandbox: ports.ServiceSandboxOn}}
	pages := NewHandler(Deps{
		Projects:       fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:          fakeTickets{[]*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		Agents:         fakeAgents{[]*domain.Agent{{ID: "a-lead", Name: "Lead dev"}}},
		Repositories:   fakeRepos{[]*domain.Repository{{ID: "r1", ProjectID: "p1", Name: "harness"}}},
		Sessions:       fakeSessions{},
		Services:       fake,
		ServiceCounter: fixedRunning(2),
	}, assets, nil)
	feed := &sseFeed{}
	mux := http.NewServeMux()
	mux.Handle("/api/services/events", feed)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	item := `document.querySelector('nav[aria-label=Project] [data-page=services]')`
	badge := `(() => { const b = ` + item + `.querySelector('[data-nav-running]'); return b.style.display === 'none' ? '' : b.textContent.trim(); })()`
	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	var rendered string
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t1"),
		poll(item+` !== null`),
		chromedp.Evaluate(item+`.dataset.runningServices`, &rendered),
		poll(badge+` === '2 running'`),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if rendered != "2" {
		t.Errorf("data-running-services = %q, want 2", rendered)
	}
	waitFollowers(t, feed)
	feed.pushRaw(t, ports.ServiceEvent{Seq: 2, Kind: ports.ServiceDeleted, ServiceID: "s1", RunningCount: 0})
	if err := chromedp.Run(ctx, poll(badge+` === ''`)); err != nil {
		t.Fatalf("the badge did not clear at zero: %v\nJS errors: %v", err, errs.all())
	}
	feed.pushRaw(t, ports.ServiceEvent{Seq: 3, Kind: ports.ServiceDeleted, ServiceID: "s2", RunningCount: 140})
	if err := chromedp.Run(ctx, poll(badge+` === '99+ running'`)); err != nil {
		t.Fatalf("the badge did not cap at 99+: %v\nJS errors: %v", err, errs.all())
	}
}

// fixedRunning answers every project's running count with one number.
type fixedRunning int

func (f fixedRunning) RunningServiceCount(context.Context, string) (int, error) { return int(f), nil }

// Start and Stop from the page: the row says what is in flight, the feed and
// the answer both land on the same state, and a refused Start shows the
// server's message and code.
func TestServicesPageStartsAndStopsAServiceAndShowsARefusal(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	fake := &fakeServices{list: ports.ServiceList{
		Services: []*domain.Service{svc("s1", "api", domain.ServiceStopped, nil), svc("s2", "web", domain.ServiceStopped, nil)},
		Seq:      1, Runtime: ports.ServiceRuntimeInfo{Name: "docker", Available: true}, Sandbox: ports.ServiceSandboxOn,
	}}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:    fakeTickets{},
		Sessions: fakeSessions{},
		Services: fake,
	}, assets, nil)
	feed := &sseFeed{}
	seq := int64(1)
	run := &domain.ServiceRun{ID: "r1", StartedAt: time.Now().UTC().Format(time.RFC3339), StartedBy: domain.ServiceStartedBy{Kind: "person"}, Where: domain.ServiceWhere{Kind: "main", Branch: "main"}, Sandboxed: true}
	change := func(t *testing.T, id, name string, status domain.ServiceStatus, run *domain.ServiceRun) *domain.Service {
		seq++
		s := svc(id, name, status, run)
		feed.pushRaw(t, ports.ServiceEvent{Seq: seq, Kind: ports.ServiceChanged, Service: s, RunningCount: 1})
		return s
	}
	mux := http.NewServeMux()
	mux.Handle("/api/services/events", feed)
	mux.HandleFunc("POST /api/start_service", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ServiceID string `json:"service_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.ServiceID == "s2" {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]string{"error": "The sandbox is unavailable on this machine, so nothing was started.", "code": "SANDBOX_UNAVAILABLE"})
			return
		}
		s := change(t, "s1", "api", domain.ServiceRunning, run) // the feed's echo comes before the answer
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, map[string]any{"service": s, "already_running": false})
	})
	mux.HandleFunc("POST /api/stop_service", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"service": change(t, "s1", "api", domain.ServiceStopped, run)})
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	row := func(name string) string { return `document.querySelector('[data-service][data-name=` + name + `]')` }
	visible := func(name, action string) string {
		return `(() => { const b = ` + row(name) + `.querySelector('[data-action=` + action + `]'); return b && b.checkVisibility(); })()`
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/services"),
		poll(visible("api", "start")),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	waitFollowers(t, feed)
	steps := []struct {
		name string
		act  chromedp.Action
	}{
		{"click start", chromedp.Click(`[data-service][data-name=api] [data-action=start]`, chromedp.ByQuery)},
		{"running", poll(row("api") + `.querySelector('[data-service-status]').textContent === 'running'`)},
		{"stop and restart shown", poll(visible("api", "stop") + ` && ` + visible("api", "restart"))},
		{"badge", poll(`document.querySelector('[data-running-badge]').textContent === '1 running'`)},
		{"stop enabled once the start answered", poll(row("api") + `.querySelector('[data-action=stop]').disabled === false`)},
		{"click stop", chromedp.Click(`[data-service][data-name=api] [data-action=stop]`, chromedp.ByQuery)},
		{"stopped", poll(row("api") + `.querySelector('[data-service-status]').textContent === 'stopped' && ` + visible("api", "start"))},
	}
	for _, step := range steps {
		if err := chromedp.Run(ctx, step.act); err != nil {
			var dump string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-services]').innerText`, &dump))
			t.Fatalf("%s: %v\nJS errors: %v\npage: %s", step.name, err, errs.all(), dump)
		}
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-service][data-name=web] [data-action=start]`, chromedp.ByQuery),
		poll(`(() => { const b = document.querySelector('[data-services-refusal]'); return !!b && b.getAttribute('role') === 'alert' && b.textContent.includes('web did not start.') && b.textContent.includes('SANDBOX_UNAVAILABLE') && b.textContent.includes('nothing was started'); })()`),
	); err != nil {
		t.Fatalf("refusal: %v\nJS errors: %v", err, errs.all())
	}
}

// A service's log beside the list: the last part of the log, harness lines set
// apart, new output following live, a place kept while scrolled up, and a
// separator when a new run clears the log.
func TestServicesPageShowsAServiceLogAndFollowsIt(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	run := &domain.ServiceRun{ID: "r1", StartedAt: time.Now().UTC().Format(time.RFC3339), StartedBy: domain.ServiceStartedBy{Kind: "person"}, Where: domain.ServiceWhere{Kind: "main", Branch: "main"}, Sandboxed: true}
	fake := &fakeServices{list: ports.ServiceList{
		Services: []*domain.Service{svc("s1", "api", domain.ServiceRunning, run)},
		Seq:      1, Runtime: ports.ServiceRuntimeInfo{Name: "docker", Available: true}, Sandbox: ports.ServiceSandboxOn,
	}}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:    fakeTickets{},
		Sessions: fakeSessions{},
		Services: fake,
	}, assets, nil)
	feed, logFeed := &sseFeed{}, &sseFeed{}
	first := "[harness] command: make air\n"
	var lines string
	for i := 0; i < 60; i++ {
		lines += "line " + strconv.Itoa(i) + "\n"
	}
	mux := http.NewServeMux()
	mux.Handle("/api/services/events", feed)
	mux.Handle("/api/services/log", logFeed)
	mux.HandleFunc("/api/read_service_log", func(w http.ResponseWriter, r *http.Request) {
		text := first + lines
		writeJSON(w, map[string]any{"text": text, "offset": 0, "next_offset": len(text), "first_offset": 0, "run_id": "r1"})
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	poll := func(expr string) chromedp.Action {
		return chromedp.Poll(expr, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	log := `document.querySelector('[data-log]')`
	texts := `[...document.querySelectorAll('[data-log-line]')].map(l => l.textContent)`
	atBottom := log + `.scrollHeight - ` + log + `.scrollTop - ` + log + `.clientHeight <= 8`
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 600),
		chromedp.Navigate(srv.URL+"/projects/p1/services"),
		poll(`!!document.querySelector('[data-action=log]')`),
		chromedp.Click(`[data-service][data-name=api] [data-action=log]`, chromedp.ByQuery),
		poll(`document.querySelectorAll('[data-log-line]').length === 61`),
		poll(`document.querySelector('[data-log-line][data-harness] b').textContent === '[harness]'`),
		poll(atBottom),
		poll(`document.querySelector('[data-log-follow]').textContent === 'Following'`),
		poll(`document.querySelector('[data-service][data-name=api]').getAttribute('aria-current') === 'true'`),
	); err != nil {
		t.Fatalf("opening the log: %v\nJS errors: %v", err, errs.all())
	}
	logPath := len(first) + len(lines)
	waitFollowers(t, logFeed)
	logFeed.pushRaw(t, map[string]any{"kind": "chunk", "run_id": "r1", "offset": logPath, "text": "fresh one\n"})
	if err := chromedp.Run(ctx,
		poll(`(`+texts+`).at(-1) === 'fresh one'`),
		poll(atBottom),
	); err != nil {
		t.Fatalf("following a new line: %v\nJS errors: %v", err, errs.all())
	}
	logPath += len("fresh one\n")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(log+`.scrollTop = 0`, nil),
		poll(`document.querySelector('[data-log-follow]').textContent === 'Paused · scrolled up'`),
	); err != nil {
		t.Fatalf("scrolling up: %v\nJS errors: %v", err, errs.all())
	}
	logFeed.pushRaw(t, map[string]any{"kind": "chunk", "run_id": "r1", "offset": logPath, "text": "while away\n"})
	if err := chromedp.Run(ctx,
		poll(`document.body.textContent.includes('1 new line below')`),
		poll(log+`.scrollTop === 0`),
	); err != nil {
		t.Fatalf("a paused view keeps its place: %v\nJS errors: %v", err, errs.all())
	}
	logFeed.pushRaw(t, map[string]any{"kind": "reset", "run_id": "r2"})
	if err := chromedp.Run(ctx,
		poll(`document.querySelectorAll('[data-log-line]').length === 1 && document.querySelector('[data-log-line]').textContent.includes('the log was cleared')`),
		poll(`document.querySelector('[data-log-follow]').textContent === 'Following'`),
	); err != nil {
		t.Fatalf("a new run: %v\nJS errors: %v", err, errs.all())
	}
}
