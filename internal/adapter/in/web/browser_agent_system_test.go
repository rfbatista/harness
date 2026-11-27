package web

import (
	"encoding/json"
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
}

func newAgentSystemAPI(repos ...string) *agentSystemAPI {
	a := &agentSystemAPI{project: map[string]any{}, repo: map[string]map[string]any{}}
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
		"score": map[string]any{"trajectory_recording": map[string]any{"default": a.field(repo, "score.trajectory_recording.default", false)}},
	}
	values := a.project
	if repo != "" {
		values = a.repo[repo]
		if repo == a.repoList[0]["id"] {
			eff["missing_required_gates"] = []string{"secret_scan"}
		}
	}
	return map[string]any{"effective": eff, "values": values}
}

func (a *agentSystemAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/list_repositories" {
		json.NewEncoder(w).Encode(map[string]any{"repositories": a.repoList})
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
	mux.Handle("/api/projects/p1/", api)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", NewHandler(Deps{
		Projects: fakeProjects{w.projects}, Tasks: fakeTickets{w.tickets}, Sessions: fakeSessions{w.sessions},
		Agents: fakeAgents{w.agents}, Repositories: fakeRepos{w.repos}, EnvFiles: w.env,
		Now: func() time.Time { return now },
	}, assets, nil))
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
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
	var barText, layers, source, valueAfter, scopeAddress, action, replaced, dot string
	var shot []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(srv.URL+"/projects/p1/agent-system"),
		chromedp.Poll(`!!document.querySelector('.ledger-row[data-path]')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`[...document.querySelectorAll('.as-layer > header h2')].map(h => h.textContent).join()`, &layers),
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
	if layers != "Learn,Guard,Score" {
		t.Errorf("layers %q", layers)
	}
	if !strings.Contains(source, "harness default") {
		t.Errorf("source of an untouched row %q", source)
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
	var banner, missingRow, locks, switches, retryError, barText, chips, indexDot string
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
		set(`input[aria-label="Secret scan command"]`, "gitleaks detect"),
		chromedp.Poll(`!document.querySelector('[data-missing-gates]')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		// A chip is added with Enter and removed with its ×.
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
	for _, want := range []string{"Secret scan missing", "gitleaks not installed", "REQUIRED_GATE_MISSING"} {
		if !strings.Contains(banner, want) {
			t.Errorf("the red banner reads %q, want it to say %q", banner, want)
		}
	}
	if !strings.Contains(missingRow, "no command at any level") {
		t.Errorf("the missing row reads %q", missingRow)
	}
	if locks != "5" {
		t.Errorf("%s required gates say always on, want 5", locks)
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
