//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
)

// instructionsRig is the real app behind an HTTPS test server: the API, the web
// pages, a repository whose checkout is a real git repository on main, and a
// saved architecture record (the Instructions tab needs one).
type instructionsRig struct {
	srv      *httptest.Server
	checkout string
	repoID   string
	page     string // the Instructions tab
}

func startInstructionsRig(t *testing.T) *instructionsRig {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	dir := t.TempDir()
	checkout := filepath.Join(dir, "widgets")
	gitInit(t, checkout)
	var (
		cat  catalog.Catalog
		orch *orchestration.Service
		plan *planning.Service
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&cat, &orch, &plan),
		fx.NopLogger,
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	p, err := cat.Projects.CreateProject(ctx, "widgets", checkout)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := cat.Projects.CreateRepository(ctx, p.ID, "widgets", "", "file://"+checkout, checkout)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: cat.Projects, Agents: cat.Agents, Architecture: cat.Architecture,
		RepositoryArchitectures: cat.RepositoryArchitectures, Sessions: orch, Planning: plan,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: cat.Projects, Tasks: plan, Sessions: orch, Agents: cat.Agents, Repositories: cat.Projects, EnvFiles: cat.Projects,
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true // the page holds an event stream open
	srv.StartTLS()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	r := &instructionsRig{srv: srv, checkout: checkout, repoID: repo.ID,
		page: srv.URL + "/projects/" + p.ID + "/repositories/" + repo.ID + "/architecture/instructions"}
	status, out := r.post("save_repository_architecture", map[string]any{"repository_id": repo.ID, "base_version": 0, "record": map[string]any{
		"version": 1, "repository": "widgets", "pattern": "hexagonal",
		"instructions": []any{}, "tools": []any{}, "sandboxes": []any{}, "layers": []any{},
		"orchestration": map[string]any{"routing": []any{}, "handoffs": []any{}},
	}})
	if status != http.StatusOK {
		t.Fatalf("save the record: %d %v", status, out)
	}
	return r
}

func (r *instructionsRig) post(verb string, body any) (int, map[string]any) {
	raw, _ := json.Marshal(body)
	res, err := r.srv.Client().Post(r.srv.URL+"/api/"+verb, "application/json", bytes.NewReader(raw))
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// jsLiteral is s as a JavaScript string literal, newlines included.
func jsLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// file reads a harness copy through the API: its text and version.
func (r *instructionsRig) file(path string) (string, float64) {
	_, out := r.post("get_instruction_file", map[string]any{"repository_id": r.repoID, "path": path})
	text, _ := out["content"].(string)
	v, _ := out["version"].(float64)
	return text, v
}

// The Instructions tab against the server as it runs, on a real git checkout,
// with a headless Chrome as the person: create a file, edit it, lose a race
// with a save from elsewhere, import the repository's copy, remove it from the
// repository and see the commit. The tab's own behaviour is covered in detail
// against a fake in internal/adapter/in/web; this proves the two sides agree.
func TestInstructionFilesInTheBrowserAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	r := startInstructionsRig(t)
	me := newPerson(t)
	poll := func(js string) chromedp.Action {
		return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10_000_000_000))
	}
	const panel = `#architecture-panel-instructions `
	q := func(sel string) string { return `document.querySelector(` + jsString(panel+sel) + `)` }
	click := func(sel string) chromedp.Action {
		return poll(`(() => { const e = ` + q(sel) + `; if (!e || e.disabled) return false; e.click(); return true; })()`)
	}
	shown := func(sel string) string { return `(` + q(sel) + `?.checkVisibility() ?? false)` }
	text := panel + `textarea[data-file-text]`
	typeText := func(s string) chromedp.Action {
		return chromedp.Evaluate(`(() => { const ta = `+q(`textarea[data-file-text]`)+`; ta.value = `+jsLiteral(s)+`; ta.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil)
	}
	value := func(sel string) string { return q(sel) + `?.value` }
	version := q(`[data-file-version]`) + `?.textContent.trim()`
	var got string

	// Create CLAUDE.md with Add file and Save.
	me.run(t, "open the tab",
		chromedp.Navigate(r.page),
		poll(shown(`[data-add]`)),
		click(`[data-add]`),
		poll(shown(`[data-new-file]`)+` && `+shown(`textarea[data-file-text]`)),
		poll(value(`[data-new-path]`)+` === 'CLAUDE.md'`),
		typeText("# Rules\n\nFirst.\n"),
		click(`[data-save]`),
		poll(version+` === 'Version 1'`),
		poll(`[...document.querySelectorAll(`+jsString(panel+`[data-entry] [data-path]`)+`)].some(p => p.textContent.trim() === 'CLAUDE.md')`),
	)
	if text, v := r.file("CLAUDE.md"); text != "# Rules\n\nFirst.\n" || v != 1 {
		t.Fatalf("created file = %q v%v", text, v)
	}

	// Edit and save a new version.
	me.run(t, "edit and save",
		typeText("# Rules\n\nSecond.\n"),
		poll(shown(`[data-dirty]`)),
		click(`[data-save]`),
		poll(version+` === 'Version 2'`),
		poll(`!`+shown(`[data-dirty]`)),
	)
	if text, v := r.file("CLAUDE.md"); text != "# Rules\n\nSecond.\n" || v != 2 {
		t.Fatalf("edited file = %q v%v", text, v)
	}

	// A stale save: the file changes from outside between open and save. The
	// refusal keeps the person's text and offers Reload and the diff.
	me.run(t, "type without saving", typeText("# Rules\n\nMine.\n"), poll(shown(`[data-dirty]`)))
	if status, out := r.post("save_instruction_file", map[string]any{"repository_id": r.repoID, "path": "CLAUDE.md", "kind": "memory", "base_version": 2, "content": "# Rules\n\nTheirs.\n"}); status != http.StatusOK {
		t.Fatalf("save from outside: %d %v", status, out)
	}
	me.run(t, "save loses the race",
		click(`[data-save]`),
		poll(shown(`[data-newer-notice]`)),
		chromedp.Evaluate(`document.querySelector(`+jsString(text)+`).value`, &got),
		poll(shown(`[data-reload]`)+` && `+shown(`[data-show-diff]`)),
		click(`[data-show-diff]`),
		poll(shown(`[data-instruction-diff]`)),
	)
	if got != "# Rules\n\nMine.\n" {
		t.Fatalf("the text after a stale save = %q", got)
	}
	var diff string
	me.run(t, "read the diff", chromedp.Evaluate(`JSON.stringify([...document.querySelectorAll(`+jsString(panel+`[data-instruction-diff] [data-diff-line]`)+`)].map(l => [l.dataset.diffLine, l.textContent.trim()]))`, &diff))
	if !strings.Contains(diff, `["del","Mine."]`) || !strings.Contains(diff, `["ins","Theirs."]`) {
		t.Fatalf("diff = %s", diff)
	}
	if text, v := r.file("CLAUDE.md"); text != "# Rules\n\nTheirs.\n" || v != 3 {
		t.Fatalf("the stale save changed the file: %q v%v", text, v)
	}
	me.run(t, "reload",
		click(`[data-reload]`),
		poll(value(`textarea[data-file-text]`)+` === `+jsLiteral("# Rules\n\nTheirs.\n")),
		poll(version+` === 'Version 3'`),
		poll(`!`+shown(`[data-newer-notice]`)+` && !`+shown(`[data-dirty]`)),
	)

	// The default branch commits a CLAUDE.md of its own: the banner lists it as
	// differing, Import brings it in as version 4 and it reads the same.
	if err := os.WriteFile(filepath.Join(r.checkout, "CLAUDE.md"), []byte("# From git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitE2E(t, r.checkout, "add", "CLAUDE.md")
	gitE2E(t, r.checkout, "commit", "-q", "-m", "rules in git")
	const banner = `[data-repository-banner] `
	row := banner + `[data-repository-copy][data-copy-path="CLAUDE.md"] `
	me.run(t, "the banner",
		chromedp.Navigate(r.page),
		poll(shown(row)),
		poll(q(row+`[data-copy-mark]`)+`?.textContent.trim() === 'differs'`),
		click(banner+`[aria-label="Import CLAUDE.md"]`),
		poll(q(row+`[data-copy-mark]`)+`?.textContent.trim() === 'same'`),
	)
	if text, v := r.file("CLAUDE.md"); text != "# From git\n" || v != 4 {
		t.Fatalf("imported file = %q v%v", text, v)
	}

	// Remove from the repository: one commit on main, the file gone from the tree.
	before := gitE2E(t, r.checkout, "rev-parse", "HEAD")
	me.run(t, "remove from the repository",
		click(banner+`[aria-label="Remove CLAUDE.md from the repository"]`),
		poll(shown(banner+`[data-remove-confirm]`)),
		click(banner+`[data-confirm-remove]`),
		poll(shown(banner+`[data-removed-commit]`)),
		poll(`!`+q(row)),
	)
	var note string
	me.run(t, "the commit note", chromedp.Evaluate(q(banner+`[data-removed-commit]`)+`.textContent.replace(/\s+/g, ' ')`, &note))
	head := gitE2E(t, r.checkout, "rev-parse", "HEAD")
	if head == before || gitE2E(t, r.checkout, "rev-list", "--count", before+"..HEAD") != "1" {
		t.Fatalf("head %s, before %s: want one new commit", head, before)
	}
	if !strings.Contains(note, head[:7]) {
		t.Errorf("the page's commit note %q lacks the short hash %s", note, head[:7])
	}
	if msg := gitE2E(t, r.checkout, "log", "-1", "--format=%s"); msg != "chore(architecture): move instruction files into the harness" {
		t.Errorf("commit message = %q", msg)
	}
	if tree := gitE2E(t, r.checkout, "ls-tree", "-r", "--name-only", "HEAD"); strings.Contains(tree, "CLAUDE.md") {
		t.Errorf("CLAUDE.md is still in the tree:\n%s", tree)
	}
	if text, v := r.file("CLAUDE.md"); text != "# From git\n" || v != 4 {
		t.Errorf("harness copy after removal = %q v%v", text, v)
	}
	if st := gitE2E(t, r.checkout, "status", "--porcelain"); st != "" {
		t.Errorf("the checkout is not clean: %s", st)
	}
	if errs := me.errors; len(errs) > 0 {
		t.Errorf("JavaScript errors: %v", errs)
	}
}
