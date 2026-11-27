package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// agentSystemAPI is the agent-system API of one project in memory: what the project and one repository set,
// and one repository missing a required gate. It answers the shapes of the contract.
type agentSystemAPI struct {
	mu       sync.Mutex
	project  map[string]any
	repo     map[string]map[string]any
	patches  []string
	refuse   []map[string]string // the next save is refused with these errors (422)
	repoList []map[string]string
	checkSh  map[string][]map[string]string // each repository's check.sh import preview
	stream   *agentSystemStream
}

// agentSystemStream is the project's event feed (GET /api/events) in memory: the page's stream follows it through
// the test bridge, and it sends a message when a test says so.
type agentSystemStream struct {
	mu    sync.Mutex
	conns []chan []byte
}

// followers is how many open connections the feed has.
func (s *agentSystemStream) followers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *agentSystemStream) push(data any) {
	b, _ := json.Marshal(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c <- b
	}
}

func (s *agentSystemStream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	flusher.Flush()
	c := make(chan []byte, 16)
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()
	for {
		select {
		case b := <-c:
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// set saves a field at a level, as another page would: "" is the project.
func (a *agentSystemAPI) set(repo, path string, value any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if repo == "" {
		a.project[path] = value
	} else {
		a.repo[repo][path] = value
	}
}

func newAgentSystemAPI(repos ...string) *agentSystemAPI {
	a := &agentSystemAPI{project: map[string]any{}, repo: map[string]map[string]any{}, stream: &agentSystemStream{}}
	for _, r := range repos {
		a.repoList = append(a.repoList, map[string]string{"id": r, "name": r, "project_id": "p1"})
		a.repo[r] = map[string]any{}
	}
	return a
}

func (a *agentSystemAPI) field(repo, path string, def any) map[string]any {
	if repo != "" {
		if v, ok := a.repo[repo][path]; ok {
			return map[string]any{"value": v, "source": "repository"}
		}
	}
	if v, ok := a.project[path]; ok {
		return map[string]any{"value": v, "source": "project"}
	}
	return map[string]any{"value": def, "source": "harness"}
}

// gates are the required kinds with a detected command; the first repository has no secret scan (gitleaks is not
// installed), and a level that sets a gate's command or timeout shows it.
func (a *agentSystemAPI) gates(repo string) []map[string]any {
	detected := map[string]string{"build": "go build ./...", "test": "go test ./...", "lint": "go vet ./...", "secret_scan": "gitleaks detect --source .", "lockfile": "go mod verify"}
	var out []map[string]any
	for _, kind := range []string{"build", "test", "lint", "secret_scan", "lockfile"} {
		command := map[string]any{"value": detected[kind], "source": "harness", "reason": "detected from go.mod"}
		if repo != "" && repo == a.repoList[0]["id"] && kind == "secret_scan" {
			command = map[string]any{"value": "", "source": "harness", "reason": "gitleaks not installed"}
		}
		if v, ok := a.project["guard.gates."+kind+".command"]; ok {
			command = map[string]any{"value": v, "source": "project"}
		}
		if v, ok := a.repo[repo]["guard.gates."+kind+".command"]; ok && repo != "" {
			command = map[string]any{"value": v, "source": "repository"}
		}
		out = append(out, map[string]any{"kind": kind, "command": command, "timeout_seconds": a.field(repo, "guard.gates."+kind+".timeout_seconds", 600)})
	}
	return out
}

func (a *agentSystemAPI) view(repo string) map[string]any {
	eff := map[string]any{
		"guard": map[string]any{
			"gates":           a.gates(repo),
			"protected_paths": a.field(repo, "guard.protected_paths", []string{"check.sh", ".github/workflows/**"}),
			"lockfile_rule":   map[string]any{"enabled": a.field(repo, "guard.lockfile_rule.enabled", true)},
			"retry_limit":     a.field(repo, "guard.retry_limit", 3),
			"tests_fail_first": map[string]any{"available": a.field(repo, "guard.tests_fail_first.available", false)},
		},
		"learn": map[string]any{
			"context_map": map[string]any{"enabled": a.field(repo, "learn.context_map.enabled", true)},
			"task_memory": map[string]any{"require_update": a.field(repo, "learn.task_memory.require_update", true)},
		},
		"review": map[string]any{
			"critic": map[string]any{
				"enabled":   a.field(repo, "review.critic.enabled", true),
				"model":     a.field(repo, "review.critic.model", "opus"),
				"standards": a.field(repo, "review.critic.standards", []string{"AGENTS.md", "CLAUDE.md", "context map"}),
			},
			"visual_qa": map[string]any{"ui": a.field(repo, "review.visual_qa.ui", nil)},
		},
		"score": map[string]any{"trajectory_recording": map[string]any{"default": a.field(repo, "score.trajectory_recording.default", false)}},
	}
	values := a.project
	if repo != "" {
		values = a.repo[repo]
		if repo == a.repoList[0]["id"] {
			eff["missing_required_gates"] = []string{"secret_scan"}
		}
	}
	return map[string]any{"effective": eff, "values": values, "updated_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
}

func (a *agentSystemAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/list_repository_run_commands" {
		json.NewEncoder(w).Encode(map[string]any{"run_commands": []map[string]string{{"name": "web", "command": "make run"}, {"name": "docs", "command": "make docs"}}})
		return
	}
	if r.URL.Path == "/api/projects/p1/bench/scorecards" {
		json.NewEncoder(w).Encode(map[string]any{"scorecards": []map[string]any{{"status": "finished", "aggregate": map[string]any{"mean": 0.82}, "cost_usd": 1.14, "cases": []any{1, 2, 3}}}})
		return
	}
	if r.URL.Path == "/api/list_repositories" {
		json.NewEncoder(w).Encode(map[string]any{"repositories": a.repoList})
		return
	}
	if repo, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/api/projects/p1/repositories/"), "/agent-system/check-sh-import"); ok {
		gates := a.checkSh[repo]
		if gates == nil {
			gates = []map[string]string{}
		}
		json.NewEncoder(w).Encode(map[string]any{"gates": gates})
		return
	}
	path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/projects/p1"), "/agent-system")
	repo := strings.TrimPrefix(path, "/repositories/")
	if path == "" {
		repo = ""
	}
	if r.Method == http.MethodPatch {
		var patch map[string]any
		json.NewDecoder(r.Body).Decode(&patch)
		a.patches = append(a.patches, r.URL.Path+" "+toJSON(patch))
		if a.refuse != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{"errors": a.refuse})
			a.refuse = nil
			return
		}
		target := a.project
		if repo != "" {
			target = a.repo[repo]
		}
		for k, v := range patch {
			if v == nil {
				delete(target, k)
			} else {
				target[k] = v
			}
		}
	}
	json.NewEncoder(w).Encode(a.view(repo))
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func (a *agentSystemAPI) patchLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.patches...)
}

func agentSystemServer(t *testing.T, api *agentSystemAPI) *httptest.Server {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	w := board()
	mux := http.NewServeMux()
	mux.Handle("/api/list_repositories", api)
	mux.Handle("/api/list_repository_run_commands", api)
	mux.Handle("/api/events", api.stream)
	mux.Handle("/api/projects/p1/", api)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the page's event stream never ends on its own
		srv.Close()
	})
	return srv
}

// TestAgentSystemPageInTheBrowser: the page shows the project defaults as ledger rows with their sources,
// a toggle becomes an unsaved change that Save sends as one PATCH, the scope switch moves to a repository (and the address
// follows), where an inherited row offers Override and an override offers Reset, and a repository missing a
// required gate carries the red dot.
func TestAgentSystemPageInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	row := func(path string) string { return `document.querySelector('.ledger-row[data-path="` + path + `"]')` }
	var barText, layers, source, valueAfter, scopeAddress, action, replaced, dot, toolbarNote string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.as-layer > header h2')].map(h => h.textContent).join()`, &layers),
		chromedp.Evaluate(`document.querySelector('.toolbar [role=status]').textContent`, &toolbarNote),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.src').textContent`, &source),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('[role=switch]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.save-bar').textContent`, &barText),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`(`+row("learn.context_map.enabled")+`.querySelector('.src').textContent || '').includes('project')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('[role=switch]').getAttribute('aria-checked')`, &valueAfter),

		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:last-child').click()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(`!!document.querySelector('.toolbar .status[data-state=missing]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('repository')`, &scopeAddress),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.act').textContent`, &action),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.act button').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`(`+row("learn.context_map.enabled")+`.querySelector('.act').textContent || '') === 'Reset'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(row("learn.context_map.enabled")+`.querySelector('.src').textContent`, &replaced),
		chromedp.Evaluate(`document.querySelector('.toolbar .status[data-state=missing]')?.getAttribute('aria-label') ?? ''`, &dot),
		chromedp.FullScreenshot(&shot, 90),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if layers != "Learn,Guard,Review,Score" {
		t.Errorf("layers %q", layers)
	}
	if !strings.Contains(source, "harness default") {
		t.Errorf("source of an untouched row %q", source)
	}
	if !strings.Contains(toolbarNote, "saved 1 h ago") {
		t.Errorf("the toolbar before a save reads %q, want the last save's age", toolbarNote)
	}
	if !strings.Contains(barText, "1 unsaved change") || !strings.Contains(barText, "Save project defaults") {
		t.Errorf("the save bar reads %q", barText)
	}
	if valueAfter != "false" {
		t.Errorf("the toggle reads %q after saving, want false", valueAfter)
	}
	if scopeAddress != "harness" {
		t.Errorf("?repository= is %q, want harness (the first repository)", scopeAddress)
	}
	if action != "Override" {
		t.Errorf("an inherited row's action is %q, want Override", action)
	}
	if !strings.Contains(replaced, "override") || !strings.Contains(replaced, "project: off") {
		t.Errorf("an override reads %q, want it to name the project value it replaces", replaced)
	}
	if dot != "missing a required gate" {
		t.Errorf("red dot %q", dot)
	}
	patches := api.patchLog()
	want := []string{
		`/api/projects/p1/agent-system {"learn.context_map.enabled":false}`,
		`/api/projects/p1/repositories/harness/agent-system {"learn.context_map.enabled":false}`,
	}
	if len(patches) != 2 || patches[0] != want[0] || patches[1] != want[1] {
		t.Errorf("patches %q, want %q", patches, want)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/agent-system.jpg", shot, 0o644)
	}
}

// TestAgentSystemPageOpensTheRepositoryTheAddressNames: ?repository= picks the scope on load, and Back
// returns to the project defaults.
func TestAgentSystemPageOpensTheRepositoryTheAddressNames(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var selected, afterBack string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]') && !!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('select[aria-label=Repository]').value`, &selected),
		chromedp.Evaluate(`document.querySelector('.toolbar [aria-label=Scope] button:first-child').click()`, nil),
		chromedp.Poll(`!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('select[aria-label=Repository]').value`, &afterBack),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if selected != "blog" || afterBack != "blog" {
		t.Errorf("selected %q, after Back %q, want blog both times", selected, afterBack)
	}
}

// TestAgentSystemRefusedSaveLandsOnTheRowInTheBrowser: a 422 puts its code and message on the named row and
// in the bar, keeps the person's value and the draft, and Save stays disabled until the row changes; Discard
// throws the draft away.
func TestAgentSystemRefusedSaveLandsOnTheRowInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	row := `document.querySelector('.ledger-row[data-path="score.trajectory_recording.default"]')`
	var rowError, bar, banner, switchValue, saveDisabled string
	var afterEdit, barStays bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(row+`.querySelector('[role=switch]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	api.mu.Lock()
	api.refuse = []map[string]string{{"field": "score.trajectory_recording.default", "code": "INVALID_TYPE", "message": "takes true or false"}}
	api.mu.Unlock()
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`!!`+row+`.querySelector('.err')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(row+`.querySelector('.err').textContent`, &rowError),
		chromedp.Evaluate(`document.querySelector('.save-bar').textContent`, &bar),
		chromedp.Evaluate(`document.querySelector('[data-refused]').textContent`, &banner),
		chromedp.Evaluate(row+`.querySelector('[role=switch]').getAttribute('aria-checked')`, &switchValue),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').getAttribute('aria-disabled')`, &saveDisabled),
		chromedp.Evaluate(row+`.querySelector('[role=switch]').click()`, nil),
		chromedp.Poll(`!`+row+`.querySelector('.err')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.body.textContent.includes('Not saved.')`, &afterEdit),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(rowError, "INVALID_TYPE") || !strings.Contains(rowError, "takes true or false") {
		t.Errorf("row error %q", rowError)
	}
	if !strings.Contains(bar, "1 error") || !strings.Contains(bar, "Score › Trajectory recording") {
		t.Errorf("the bar reads %q", bar)
	}
	if !strings.Contains(banner, "INVALID_TYPE · score.trajectory_recording.default") {
		t.Errorf("banner %q", banner)
	}
	if switchValue != "true" {
		t.Errorf("the person's value was lost: %q", switchValue)
	}
	if saveDisabled != "true" {
		t.Errorf("Save is not disabled while the refusal stands: %q", saveDisabled)
	}
	if afterEdit {
		t.Errorf("the refusal banner stays after the row changed")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`!!document.querySelector('.save-bar')`, &barStays),
	); err != nil {
		t.Fatal(err)
	}
	if barStays {
		t.Errorf("toggling the row back to what is saved leaves the bar")
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestAgentSystemGuardSectionInTheBrowser: the Gates list shows the required kinds with "always on" and no switch,
// a repository missing a required gate shows the red banner and the missing row, typing a command and Save sends
// the contract's gate path, a glob chip is added and removed, and a retry limit out of range is refused on its row
// before anything is sent.
func TestAgentSystemGuardSectionInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	set := func(selector, value string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const el = document.querySelector('`+selector+`'); const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set; set.call(el, '`+value+`'); el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
	}
	var banner, missingRow, locks, switches, retryError, barText, chips, indexDot, chipsAtRest, redLineBoxed, scopeControls, ownOverrides, optionalLine string
	var saveDisabled bool
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-gate]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-missing-gates]')?.textContent ?? ''`, &banner),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-gate=secret_scan]').textContent`, &missingRow),
		chromedp.Evaluate(`document.querySelectorAll('.ledger-row[data-gate] .lock').length + ''`, &locks),
		chromedp.Evaluate(`document.querySelectorAll('.ledger-row[data-gate=build] [role=switch], .ledger-row[data-gate=secret_scan] [role=switch]').length + ''`, &switches),
		chromedp.Evaluate(`document.querySelector('.as-index a[href="#as-guard"] .status')?.getAttribute('aria-label') ?? ''`, &indexDot),
		chromedp.Evaluate(`document.querySelector('[data-missing-gates]').classList.contains('banner') ? 'boxed' : 'line'`, &redLineBoxed),
		chromedp.Evaluate(`document.querySelectorAll('main header [aria-label=Scope] select').length + '/' + document.querySelectorAll('main header select').length`, &scopeControls),
		chromedp.Evaluate(`document.querySelector('[data-own-overrides]')?.textContent ?? ''`, &ownOverrides),
		chromedp.Evaluate(`(() => { const row = document.querySelector('.ledger-row[data-gate=typecheck]'); const line = row.querySelector('.gate-line'); return line.querySelector('[role=switch]') && line.querySelector('input') && !line.contains(row.querySelector('.tmo')) ? 'one line' : 'split'; })()`, &optionalLine),
		set(`input[aria-label="Secret scan command"]`, "gitleaks detect"),
		chromedp.Poll(`!document.querySelector('[data-missing-gates]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		// Chips are plain at rest (no ×, no add field); Edit opens them. A chip is added with Enter and removed with its ×.
		chromedp.Evaluate(`document.querySelectorAll('.ledger-row[data-path="guard.protected_paths"] .chip-x, .ledger-row[data-path="guard.protected_paths"] input.chip-add').length + ''`, &chipsAtRest),
		chromedp.Evaluate(`document.querySelector('button[aria-label="Edit Protected paths"]').click()`, nil),
		set(`input[aria-label="Add to Protected paths"]`, "**/*.lock"),
		chromedp.Evaluate(`document.querySelector('input[aria-label="Add to Protected paths"]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))`, nil),
		chromedp.Poll(`[...document.querySelectorAll('.ledger-row[data-path="guard.protected_paths"] .badge.glob')].some(b => b.textContent.includes('**/*.lock'))`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('button[aria-label="Remove check.sh"]').click()`, nil),
		chromedp.Poll(`![...document.querySelectorAll('.ledger-row[data-path="guard.protected_paths"] .badge.glob')].some(b => b.textContent.includes('check.sh'))`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.ledger-row[data-path="guard.protected_paths"] .badge.glob')].map(b => b.textContent.replace('×','')).join()`, &chips),
		// A retry limit out of range is refused on its row and keeps Save disabled.
		set(`.ledger-row[data-path="guard.retry_limit"] input`, "11"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path="guard.retry_limit"] .err')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-path="guard.retry_limit"] .err').textContent`, &retryError),
		chromedp.Evaluate(`document.querySelector('.save-bar').textContent`, &barText),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').getAttribute('aria-disabled') === 'true'`, &saveDisabled),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.FullScreenshot(&shot, 90),
		set(`.ledger-row[data-path="guard.retry_limit"] input`, "5"),
		chromedp.Poll(`!document.querySelector('.ledger-row[data-path="guard.retry_limit"] .err')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	for _, want := range []string{"Secret scan missing", "gitleaks not installed"} {
		if !strings.Contains(banner, want) {
			t.Errorf("the red banner reads %q, want it to say %q", banner, want)
		}
	}
	if !strings.Contains(missingRow, "no command at any level") {
		t.Errorf("the missing row reads %q", missingRow)
	}
	if locks != "0" {
		t.Errorf("%s required gates say always on at a repository's scope, want none: only Override", locks)
	}
	if redLineBoxed != "line" || scopeControls != "1/1" || chipsAtRest != "0" || optionalLine != "one line" {
		t.Errorf("red %s, scope selects %s, chip controls at rest %s, optional gate %s", redLineBoxed, scopeControls, chipsAtRest, optionalLine)
	}
	if !strings.Contains(ownOverrides, "Overrides on harness") {
		t.Errorf("the index's own overrides read %q", ownOverrides)
	}
	if switches != "0" {
		t.Errorf("a required gate has a switch (%s)", switches)
	}
	if indexDot != "1 missing" {
		t.Errorf("the Guard index reads %q", indexDot)
	}
	if chips != ".github/workflows/**,**/*.lock" {
		t.Errorf("chips %q", chips)
	}
	if !strings.Contains(retryError, "OUT_OF_RANGE") || !strings.Contains(retryError, "1–10") {
		t.Errorf("retry limit error %q", retryError)
	}
	if !strings.Contains(barText, "Guard › Retry limit") || !saveDisabled {
		t.Errorf("save bar %q, disabled %v", barText, saveDisabled)
	}
	patches := api.patchLog()
	want := `/api/projects/p1/repositories/harness/agent-system {"guard.gates.secret_scan.command":"gitleaks detect","guard.protected_paths":[".github/workflows/**","**/*.lock"],"guard.retry_limit":5}`
	if len(patches) != 1 || patches[0] != want {
		t.Errorf("patches %q, want %q", patches, want)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/agent-system-guard.jpg", shot, 0o644)
	}
}

// TestAgentSystemReviewSectionInTheBrowser: the Review section offers the review-mode model first and the models the
// harness can call, a chosen model and a declared UI are sent by one Save as the whole value, a route without a
// leading slash is refused on the block with its line before anything is sent, the project scope asks for a
// repository, and the Score section links to the Score page with the last run.
func TestAgentSystemReviewSectionInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	setInput := func(selector, value string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const el = document.querySelector('`+selector+`'); const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, `+toJSON(value)+`); el.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
	}
	var projectNote, options, scoreLine, routeError, barText, shot, modelAtRest string
	var saveDisabled bool
	var jpg []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('[data-visual-qa]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-visual-qa]').parentElement.textContent`, &projectNote),
		// The line reads the bench after the rows: wait for the read to land, not for the dash it starts as.
		chromedp.Poll(`document.querySelector('.ledger-row[data-link=score]').textContent.includes('$')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-link=score]').textContent`, &scoreLine),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=blog"),
		chromedp.Poll(`!!document.querySelector('[data-select-text]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('select[aria-label="Critic model"]').length + ''`, &modelAtRest),
		chromedp.Evaluate(`document.querySelector('button[aria-label="Change Critic model"]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label="Critic model"]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('select[aria-label="Critic model"] option')].map(o => o.value + '=' + o.textContent).join('|')`, &options),
		chromedp.Evaluate(`(() => { const el = document.querySelector('select[aria-label="Critic model"]'); Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set.call(el, 'sonnet'); el.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('button')].find(b => b.textContent.includes('Declare this repository')).click()`, nil),
		chromedp.Poll(`!!document.querySelector('input[aria-label="URL"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		setInput(`textarea[aria-label="Routes"]`, "/\nprojects/settings"),
		chromedp.Poll(`!!document.querySelector('[data-error-for="review.visual_qa.ui"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-error-for="review.visual_qa.ui"]').textContent`, &routeError),
		chromedp.Evaluate(`document.querySelector('.save-bar').textContent`, &barText),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').getAttribute('aria-disabled') === 'true'`, &saveDisabled),
		chromedp.Evaluate(`window.scrollTo(0, 0)`, nil),
		chromedp.FullScreenshot(&jpg, 90),
		setInput(`textarea[aria-label="Routes"]`, "/\n/projects/settings"),
		chromedp.Poll(`!document.querySelector('[data-error-for="review.visual_qa.ui"]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-visual-qa]').textContent`, &shot),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if modelAtRest != "0" {
		t.Errorf("the critic model is a select at rest (%s), want inline text", modelAtRest)
	}
	if !strings.Contains(projectNote, "Choose a repository to declare its UI") {
		t.Errorf("project scope reads %q", projectNote)
	}
	if !strings.Contains(scoreLine, "Score page · 3 cases · last run 0.82 · $1.14") {
		t.Errorf("the Score line reads %q", scoreLine)
	}
	if want := "=Review-mode model (opus)|default=default: the CLI chooses|fable=fable|opus=opus|sonnet=sonnet|haiku=haiku"; !strings.HasSuffix(options, want) && !strings.Contains(options, want) {
		t.Errorf("model options %q, want %q", options, want)
	}
	if !strings.Contains(routeError, "INVALID_ROUTE") || !strings.Contains(routeError, "line 2") || !saveDisabled || !strings.Contains(barText, "Review › UI declaration") {
		t.Errorf("route error %q, bar %q, disabled %v", routeError, barText, saveDisabled)
	}
	want := `/api/projects/p1/repositories/blog/agent-system {"review.critic.model":"sonnet","review.visual_qa.ui":{"routes":["/","/projects/settings"],"start_command":"make run","ui_paths":[],"url":"http://127.0.0.1:8080","viewports":["1440x900","390x844"]}}`
	if patches := api.patchLog(); len(patches) != 1 || patches[0] != want {
		t.Errorf("patches %q, want %q", patches, want)
	}
	if !strings.Contains(shot, "override") {
		t.Errorf("after the save the declaration reads %q, want it marked override", shot)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/agent-system-review.jpg", jpg, 0o644)
	}
}

// TestAgentSystemImportFromCheckShFillsTheDraftInTheBrowser: Import from check.sh sits on the Gates sub-list of a
// repository only, fills the commands the preview answers as unsaved overrides, saves nothing, and a Save sends them.
func TestAgentSystemImportFromCheckShFillsTheDraftInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	api.checkSh = map[string][]map[string]string{"harness": {
		{"kind": "build", "command": "./check.sh build", "reason": "detected from check.sh"},
		{"kind": "typecheck", "command": "./check.sh typecheck", "reason": "detected from check.sh"},
	}}
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)

	var onProject, build, typecheckOn, note, bar string
	var patchesBefore int
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[data-import-check-sh]').length + ''`, &onProject),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
		chromedp.Poll(`!!document.querySelector('[data-import-check-sh]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-import-check-sh]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('[data-import-note]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-import-note]').textContent`, &note),
		chromedp.Evaluate(`document.querySelector('input[aria-label="Build command"]').value`, &build),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-gate=typecheck] [role=switch]').getAttribute('aria-checked')`, &typecheckOn),
		chromedp.Evaluate(`document.querySelector('.save-bar')?.textContent ?? ''`, &bar),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	patchesBefore = len(api.patchLog())
	if onProject != "0" {
		t.Errorf("the project scope offers %s imports, want none", onProject)
	}
	if !strings.Contains(note, "Imported 2 gates from check.sh") || build != "./check.sh build" || typecheckOn != "true" {
		t.Errorf("note %q, build %q, typecheck on %q", note, build, typecheckOn)
	}
	if !strings.Contains(bar, "2 unsaved") || patchesBefore != 0 {
		t.Errorf("save bar %q, patches before Save %d", bar, patchesBefore)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.save-bar .end button:last-child').click()`, nil),
		chromedp.Poll(`!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	want := `/api/projects/p1/repositories/harness/agent-system {"guard.gates.build.command":"./check.sh build","guard.gates.typecheck.command":"./check.sh typecheck"}`
	if patches := api.patchLog(); len(patches) != 1 || patches[0] != want {
		t.Errorf("patches %q, want %q", patches, want)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestAgentSystemOverviewListsOverridesAndRedRepositoriesInTheBrowser: a project that saved nothing says so;
// once a repository overrides a field the index lists it with its count, and a repository missing a required
// gate is listed red with the kind; a repository in the index opens its scope.
func TestAgentSystemOverviewListsOverridesAndRedRepositoriesInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var banner, noOverrides, red, overrides, scope string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('[data-new-project]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-new-project]').textContent`, &banner),
		chromedp.Evaluate(`document.querySelector('[data-overrides]').textContent`, &noOverrides),
		chromedp.Evaluate(`document.querySelector('[data-red-repositories]').textContent`, &red),
		chromedp.Evaluate(`new Promise(r => setTimeout(r, 0))`, nil),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "Every value below is a harness default") || !strings.Contains(noOverrides, "No overrides") || !strings.Contains(red, "harness · secret scan") {
		t.Errorf("banner %q, overrides %q, red %q", banner, noOverrides, red)
	}
	api.set("blog", "guard.retry_limit", 5)
	api.set("blog", "review.critic.enabled", false)
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('[data-overrides] a')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`!!document.querySelector('[data-new-project]') + ''`, &banner),
		chromedp.Evaluate(`document.querySelector('[data-overrides]').textContent`, &overrides),
		chromedp.Evaluate(`document.querySelector('[data-overrides] a').click()`, nil),
		chromedp.Poll(`!!document.querySelector('select[aria-label=Repository]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('select[aria-label=Repository]').value`, &scope),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if banner != "false" || !strings.Contains(overrides, "blog · 2 fields") || scope != "blog" {
		t.Errorf("new-project banner %q, overrides %q, scope %q", banner, overrides, scope)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestAgentSystemChangedElsewhereKeepsTheUnsavedRowsInTheBrowser: a save another page makes raises the attention
// banner without touching the rows under the person's hands, and Reload brings it in and keeps their unsaved row.
func TestAgentSystemChangedElsewhereKeepsTheUnsavedRowsInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	retry := `document.querySelector('.ledger-row[data-path="guard.retry_limit"] input').value`
	var banner, retryBefore, retryAfter, unsaved, contextMap string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-path="learn.context_map.enabled"] [role=switch]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(retry, &retryBefore),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	// The page follows the project's feed once the stream names itself: wait for the follow, then another page saves.
	for deadline := time.Now().Add(10 * time.Second); api.stream.followers() == 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the page never followed the project feed")
		}
	}
	api.set("", "guard.retry_limit", 7)
	api.stream.push(map[string]any{"agent_system_settings": map[string]any{"project_id": "p1"}})
	err = chromedp.Run(ctx,
		chromedp.Poll(`!!document.querySelector('[data-changed-elsewhere]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('[data-changed-elsewhere]').textContent`, &banner),
		chromedp.Evaluate(retry, &retryAfter),
		chromedp.Evaluate(`[...document.querySelectorAll('[data-changed-elsewhere] button')].find(b => b.textContent === 'Reload').click()`, nil),
		chromedp.Poll(`!document.querySelector('[data-changed-elsewhere]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Poll(retry+` === '7'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.querySelector('.save-bar').textContent`, &unsaved),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-path="learn.context_map.enabled"] [role=switch]').getAttribute('aria-checked')`, &contextMap),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "changed on another page") {
		t.Errorf("banner %q", banner)
	}
	if retryBefore != "3" || retryAfter != "3" {
		t.Errorf("retry limit %q before and %q with the banner up, want 3 both (the rows are not replaced under the person)", retryBefore, retryAfter)
	}
	if !strings.Contains(unsaved, "1 unsaved change") || contextMap != "false" {
		t.Errorf("after Reload the bar reads %q and the context map switch %q, want the unsaved row kept", unsaved, contextMap)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
}

// TestAgentSystemOnAPhoneStacksTheLedgerInTheBrowser: at 390 px the page does not scroll sideways, the index is a
// row of tabs above the layers, a ledger row stacks, and the save bar stays in view while a row is edited.
func TestAgentSystemOnAPhoneStacksTheLedgerInTheBrowser(t *testing.T) {
	api := newAgentSystemAPI("harness", "blog")
	srv := agentSystemServer(t, api)
	ctx, errs := browser(t)
	var overflow bool
	var indexDirection, rowColumns string
	var barInView bool
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system?repository=harness"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-gate]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelector('.ledger-row[data-path="learn.context_map.enabled"] [role=switch]').click()`, nil),
		chromedp.Poll(`!!document.querySelector('.save-bar')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`document.documentElement.scrollWidth > window.innerWidth || [...document.querySelectorAll('.agent-system, .as-main, .ledger-row')].some(el => el.getBoundingClientRect().right > window.innerWidth + 1)`, &overflow),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.as-index')).flexDirection`, &indexDirection),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ledger-row[data-path="guard.retry_limit"]')).gridTemplateColumns.split(' ').length + ''`, &rowColumns),
		chromedp.Evaluate(`(() => { const r = document.querySelector('.save-bar').getBoundingClientRect(); return r.bottom <= window.innerHeight + 1 && r.top >= 0; })()`, &barInView),
		chromedp.FullScreenshot(&shot, 90),
	)
	if err != nil {
		t.Fatalf("%v\nJS errors: %v", err, errs.all())
	}
	if overflow {
		t.Errorf("the page scrolls sideways at 390 px")
	}
	if indexDirection != "row" || rowColumns != "2" || !barInView {
		t.Errorf("index %q, ledger row columns %q, save bar in view %v", indexDirection, rowColumns, barInView)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JS errors: %v", e)
	}
	if dir := os.Getenv("WEB_SCREENSHOT_DIR"); dir != "" {
		_ = os.WriteFile(dir+"/agent-system-phone.jpg", shot, 0o644)
	}
}
