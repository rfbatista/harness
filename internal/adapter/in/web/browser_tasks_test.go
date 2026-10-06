package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
)

// setField sets a field's value and fires the event Alpine's x-model listens
// for, as a person's edit would.
func setField(selector, value, event string) chromedp.Action {
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		el.value = %q;
		el.dispatchEvent(new Event(%q, { bubbles: true }));
		return true;
	})()`, selector, value, event)
	return chromedp.Evaluate(js, nil)
}

// fakeTicketsAPI answers the ticket writes and GET /api/sessions the way
// httpapi does, and records them. An update is partial, as the server's: a
// key absent from the body keeps the stored value.
type fakeTicketsAPI struct {
	mu       sync.Mutex
	calls    []string
	sessions map[string][]map[string]string // ticket id → sessions
	tickets  map[string]map[string]string   // ticket id → ticket, merged on update
}

func (f *fakeTicketsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/sessions" {
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": f.sessions[r.URL.Query().Get("ticket_id")]})
		return
	}
	if r.URL.Path == "/api/events" {
		http.NotFound(w, r)
		return
	}
	var in map[string]string
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	b, _ := json.Marshal(in)
	f.calls = append(f.calls, r.URL.Path+" "+string(b))
	f.mu.Unlock()
	switch r.URL.Path {
	case "/api/create_ticket":
		tk := map[string]string{"id": "t-new", "project_id": in["project_id"], "title": in["title"], "description": in["description"], "status": in["status"]}
		f.mu.Lock()
		f.tickets[tk["id"]] = tk
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ticket": tk})
	case "/api/update_ticket":
		f.mu.Lock()
		tk := f.tickets[in["ticket_id"]]
		if tk == nil {
			tk = map[string]string{"id": in["ticket_id"], "project_id": "p1", "status": "todo"}
			f.tickets[in["ticket_id"]] = tk
		}
		for _, k := range []string{"title", "description", "status"} {
			if v, ok := in[k]; ok {
				tk[k] = v
			}
		}
		out := map[string]string{}
		for k, v := range tk {
			out[k] = v
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ticket": out})
	case "/api/delete_ticket":
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeTicketsAPI) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

func TestTaskManagementInTheBrowser(t *testing.T) {
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)

	api := &fakeTicketsAPI{
		sessions: map[string][]map[string]string{
			"t-busy": {{"id": "s1", "ticket_id": "t-busy", "status": "running"}},
		},
		tickets: map[string]map[string]string{},
	}
	pages := NewHandler(Deps{
		Projects: fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks: fakeTickets{[]*domain.Ticket{
			{ID: "t-new", ProjectID: "p1", Title: "Write docs", Description: "The TUI's live section.", Status: domain.TicketStatusTodo},
			{ID: "t-busy", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		}},
		Sessions:     fakeSessions{},
		Agents:       fakeAgents{},
		Repositories: fakeRepos{},
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Create: the form posts the task, then opens it.
	var at, brief string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/new"),
		chromedp.WaitVisible(`#task-title`, chromedp.ByQuery),
		chromedp.SendKeys(`#task-title`, "Write docs", chromedp.ByQuery),
		chromedp.SendKeys(`#task-description`, "The TUI's live section.", chromedp.ByQuery),
		clickButton(`main form`, "Create task"),
		waitForPath(srv.URL+"/projects/p1/tasks/t-new", &at),
		chromedp.Poll(`document.querySelector('.brief')?.textContent === "The TUI's live section."`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.brief').textContent`, &brief),
	)
	if err != nil {
		t.Fatalf("create: %v\nAPI:\n%s\nJS errors: %v", err, api.log(), errs.all())
	}

	// Edit, then change the status (which reloads the page).
	var title string
	err = chromedp.Run(ctx,
		clickButton(`main header`, "Edit"),
		chromedp.WaitVisible(`#task-edit-title`, chromedp.ByQuery),
		setField(`#task-edit-title`, "Write the live docs", "input"),
		clickButton(`main`, "Save"),
		chromedp.Poll(`document.querySelector('main h1')?.textContent === 'Write the live docs'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('main h1').textContent`, &title),
		setField(`select.status-picker`, "review", "change"),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("edit: %v\nAPI:\n%s\nJS errors: %v", err, api.log(), errs.all())
	}

	// Delete: refused while the task has a session; allowed without.
	var refusal string
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-busy"),
		clickButton(`main header`, "Delete task"),
		chromedp.Poll(`document.querySelector('[role=alert]')?.textContent?.includes('cannot be deleted') ?? false`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[role=alert]').textContent.replace(/\s+/g, ' ')`, &refusal),
		chromedp.Navigate(srv.URL+"/projects/p1/tasks/t-new"),
		clickButton(`main header`, "Delete task"),
		clickButton(`[role=alertdialog]`, "Delete task"),
		waitForPath(srv.URL+"/projects/p1", &at),
	)
	if err != nil {
		t.Fatalf("delete: %v\nAPI:\n%s\nJS errors: %v", err, api.log(), errs.all())
	}

	want := []string{
		`/api/create_ticket {"description":"The TUI's live section.","project_id":"p1","status":"todo","title":"Write docs"}`,
		`/api/update_ticket {"description":"The TUI's live section.","ticket_id":"t-new","title":"Write the live docs"}`,
		`/api/update_ticket {"status":"review","ticket_id":"t-new"}`,
		`/api/delete_ticket {"ticket_id":"t-new"}`,
	}
	if got := api.log(); got != strings.Join(want, "\n") {
		t.Errorf("API calls:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	if !strings.Contains(refusal, "1 running session") {
		t.Errorf("refusal = %q", refusal)
	}
	if title != "Write the live docs" || brief != "The TUI's live section." {
		t.Errorf("title %q, brief %q", title, brief)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
