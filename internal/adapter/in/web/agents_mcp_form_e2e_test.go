//go:build !windows

package web_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/rfbatista/harnesskit/mcpserver"

	"operators-mcp/internal/adapter/out/agentcatalog"
)

// A fake token, shaped like a Linear key and worth nothing.
const mcpFormToken = "lin_api_" + "8Hk2xQ9vT4mZpR7wY1cD"

// mcpFormProber is the connection test's fake: it answers with two tool names
// once its gate opens, and keeps what it was asked to start.
type mcpFormProber struct {
	mu    sync.Mutex
	gate  chan struct{}
	calls []mcpserver.Server
}

func (p *mcpFormProber) Probe(ctx context.Context, s mcpserver.Server) (mcpserver.ProbeResult, error) {
	p.mu.Lock()
	p.calls = append(p.calls, s)
	p.mu.Unlock()
	select {
	case <-p.gate:
	case <-ctx.Done():
		return mcpserver.ProbeResult{Status: "error", Error: ctx.Err().Error()}, nil
	}
	if s.Command == "missing-cmd" {
		return mcpserver.ProbeResult{Status: "error", Error: `exec: "missing-cmd": executable file not found in $PATH`}, nil
	}
	return mcpserver.ProbeResult{Status: "ok", ToolCount: 2, ToolNames: []string{"search_issues", "create_issue"}}, nil
}

func (p *mcpFormProber) asked() []mcpserver.Server {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]mcpserver.Server(nil), p.calls...)
}

// mcpFormClick clicks the enabled element through the DOM, once it is there: a
// sticky footer can cover a button that a pointer click would land on.
func mcpFormClick(selector string) chromedp.Action {
	return waitUntil(`(() => { const el = document.querySelector(` + quote(selector) + `); if (!el || el.disabled) return false; el.click(); return true; })()`)
}

// mcpFormWire keeps what the page sent the server: each request's address and body.
type mcpFormWire struct {
	mu   sync.Mutex
	sent []string
}

func (w *mcpFormWire) watch(b *browser) chromedp.Action {
	chromedp.ListenTarget(b.ctx, func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok {
			text := e.Request.URL
			for _, entry := range e.Request.PostDataEntries {
				raw, err := base64.StdEncoding.DecodeString(entry.Bytes)
				if err != nil {
					raw = []byte(entry.Bytes)
				}
				text += " " + string(raw)
			}
			w.mu.Lock()
			w.sent = append(w.sent, text)
			w.mu.Unlock()
		}
	})
	return network.Enable()
}

func (w *mcpFormWire) all() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.sent, "\n")
}

// TestMcpServerFormCreatesATestedServerFromAPastedCommandLine_EndToEnd is W1
// for story 11 of spec "Web UI — the Agents page", against a real server and a
// temporary catalog with a fake probe: a server is made by pasting a command
// line, a token pasted into an environment value is stopped before it goes
// anywhere and moved to a ${VAR}, each variable shows set or not set, the
// connection test shows its wait and then the tools, an agent and a project
// are ticked, and the saved server is in the library, in the files and on the
// agent and the project.
func TestMcpServerFormCreatesATestedServerFromAPastedCommandLine_EndToEnd(t *testing.T) {
	t.Setenv("MCPFORM_E2E_SET", "1")
	prober := &mcpFormProber{gate: make(chan struct{})}
	w := newTokenSaverWorld(t, agentcatalog.WithProber(prober))
	writeCatalogFile(t, w, "agents.json", `{
  "agents": {
    "designer": {"description": "Designs screens.", "mcp_servers": ["github"]},
    "backend": {"description": "Builds the API."}
  }
}
`)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"command": "npx", "args": ["gh-mcp"], "env": {"TOKEN": "${MCPFORM_E2E_GH}"}}}}
`)
	other, err := w.cat.Projects.CreateProject(w.ctx, "billing-api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalogFile := func(rel string) string {
		raw, _ := os.ReadFile(filepath.Join(w.root, "agents", rel))
		return string(raw)
	}
	projectServers := func(id string) string {
		res, err := w.srv.Client().Get(w.srv.URL + "/api/get_project_mcp_servers?project_id=" + id)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return strings.TrimSpace(string(raw))
	}

	flat := func(sel string) string { return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()` }
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	attr := func(sel, name string) string { return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)` }
	save := `document.querySelector('[data-mcp-form] [data-save-footer] [data-save]')`
	cell := func(section string, n int, field string) string {
		return fmt.Sprintf(`[data-mf-%s] > div:nth-of-type(%d) [data-field=%s]`, section, n, field)
	}
	ref := func(name string) string {
		return `(document.querySelector('[data-mf-ref][data-ref="` + name + `"]')?.dataset.state ?? '')`
	}
	values := func(section string) string {
		return `[...document.querySelectorAll('[data-mf-` + section + `] input[data-field=value]')].map(i => i.value).join('|')`
	}
	preview := `[...document.querySelectorAll('[data-mcp-form] [data-preview-file]')].map(f => f.dataset.path).join()`
	run := func(b *browser, name string, actions ...chromedp.Action) {
		for i, a := range actions {
			b.run(fmt.Sprintf("%s [action %d]", name, i), a)
		}
	}
	wire := &mcpFormWire{}

	b := newTLSBrowser(t)
	run(b, "/agents/mcp/new is a full page: no tabs, a local command, nothing written, Save and Test as they should be",
		chromedp.EmulateViewport(1400, 1000),
		wire.watch(b),
		chromedp.Navigate(w.srv.URL+"/agents/mcp/new"),
		waitUntil(visible(`[data-mcp-form]`)),
		waitUntil(`document.querySelector('[data-mcp-form] h1').textContent.trim() === 'New MCP server'`),
		waitUntil(`!document.querySelector('[role=tablist]')?.checkVisibility()`),
		waitUntil(`document.querySelector('[data-transport=stdio]').checked === true && `+visible(`[data-mf-local]`)+` && !`+visible(`[data-mf-remote]`)),
		waitUntil(save+`.disabled === true && `+save+`.textContent.trim() === 'Add server'`),
		waitUntil(flat(`[data-mcp-form] [data-save-state]`)+` === 'Nothing is written until you add the server.'`),
		waitUntil(`document.querySelector('[data-mf-test]').disabled === true`),
		waitUntil(`[...document.querySelectorAll('[data-mf-agent-row]')].map(r => r.dataset.name).join() === 'backend,designer'`),
		waitUntil(`[...document.querySelectorAll('[data-mf-project-row]')].map(r => r.textContent.trim().split(/\s+/)[0]).join() === 'billing-api,harness'`),
		waitUntil(flat(`[data-mf-reach]`)+`.startsWith('Unused.')`),
	)
	run(b, "pasting a command line into Command splits it: the first word stays, one argument per row, quotes honoured",
		setField(`#mf-name`, "linear"),
		chromedp.Evaluate(`(() => { const el = document.querySelector('#mf-command'); el.focus();
			const dt = new DataTransfer(); dt.setData('text', 'npx -y "@linear/mcp-server" --label "two words"');
			el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true })); })()`, nil),
		waitUntil(`document.querySelector('#mf-command').value === 'npx'`),
		waitUntil(values("args")+` === '-y|@linear/mcp-server|--label|two words'`),
		waitUntil(flat(`[data-mf-note]`)+` === 'Split into a command and 4 arguments.'`),
		waitUntil(save+`.disabled === false`),
		waitUntil(preview+` === 'mcp-servers.json'`),
	)
	run(b, "a token pasted into an environment value stops there: a warning, Save and Test off, nothing sent",
		mcpFormClick(`[data-mf-add][data-section=env]`),
		waitUntil(`document.querySelectorAll('[data-mf-env] [data-mf-row]').length === 1 && document.activeElement?.dataset.field === 'key'`),
		setField(cell("env", 1, "key"), "MCPFORM_E2E_LINEAR_KEY"),
		setField(cell("env", 1, "value"), mcpFormToken),
		waitUntil(visible(`[data-mf-secret]`)),
		waitUntil(flat(`[data-mf-secret] strong`)+` === 'This looks like a Linear API key.'`),
		waitUntil(`document.querySelector('[data-mf-use]').textContent.trim() === 'Use ${MCPFORM_E2E_LINEAR_KEY} instead'`),
		waitUntil(attr(cell("env", 1, "value"), "type")+` === 'password' && `+attr(cell("env", 1, "value"), "aria-invalid")+` === 'true'`),
		waitUntil(`document.querySelector('`+cell("env", 1, "value")+`').getAttribute('aria-describedby').startsWith('mf-note-')`),
		waitUntil(`document.querySelector('[data-mf-env] select[data-mf-mode]').value === 'text'`),
		waitUntil(save+`.disabled === true`),
		waitUntil(flat(`[data-mcp-form] [data-save-state]`)+`.includes('Settle the token warning to add the server.')`),
		waitUntil(`document.querySelector('[data-mf-test]').disabled === true`),
		waitUntil(`!document.querySelector('main').innerText.includes('`+mcpFormToken+`')`),
		chromedp.Sleep(600*time.Millisecond), // longer than the preview's pause: still nothing is asked
		bothSchemes("agents-mcp-form-token-warning"),
	)
	if got := wire.all(); strings.Contains(got, mcpFormToken) {
		t.Fatalf("the token was sent while its warning was open:\n%s", got)
	}
	run(b, "Use ${VAR} instead swaps the token for a reference and says where to set it; each variable shows set or not set",
		mcpFormClick(`[data-mf-use]`),
		waitUntil(`document.querySelector('`+cell("env", 1, "value")+`').value === '${MCPFORM_E2E_LINEAR_KEY}'`),
		waitUntil(`!`+visible(`[data-mf-secret]`)+` && `+visible(`[data-mf-used]`)),
		waitUntil(flat(`[data-mf-used]`)+`.startsWith('The pasted key is gone from this form.') && `+flat(`[data-mf-used]`)+`.includes('Set MCPFORM_E2E_LINEAR_KEY to that key')`),
		waitUntil(attr(cell("env", 1, "value"), "type")+` === 'text'`),
		mcpFormClick(`[data-mf-add][data-section=env]`),
		waitUntil(`document.querySelectorAll('[data-mf-env] [data-mf-row]').length === 2`),
		setField(cell("env", 2, "key"), "SET_ONE"),
		setField(cell("env", 2, "value"), "${MCPFORM_E2E_SET}"),
		waitUntil(ref("MCPFORM_E2E_LINEAR_KEY")+` === 'not set' && `+ref("MCPFORM_E2E_SET")+` === 'set'`),
		waitUntil(visible(`[data-mf-unset]`)+` && `+flat(`[data-mf-unset]`)+`.startsWith('${MCPFORM_E2E_LINEAR_KEY} is not set in the harness server')`),
		waitUntil(save+`.disabled === false`),
		waitUntil(preview+` === 'mcp-servers.json'`),
		bothSchemes("agents-mcp-form-variables"),
	)
	t.Setenv("MCPFORM_E2E_LINEAR_KEY", "now-set")
	run(b, "Check again asks the server once more and the variable turns set",
		mcpFormClick(`[data-mf-check-again]`),
		waitUntil(ref("MCPFORM_E2E_LINEAR_KEY")+` === 'set'`),
		waitUntil(`!`+visible(`[data-mf-unset]`)),
	)
	run(b, "Test connection shows a wait while the server runs it, then the tools",
		mcpFormClick(`[data-mf-test]`),
		waitUntil(visible(`[data-mf-waiting]`)+` && `+flat(`[data-mf-waiting]`)+`.includes('Connecting…')`),
		waitUntil(`document.querySelector('[data-mf-test]').disabled === true && `+attr(`[data-mf-test]`, "aria-busy")+` === 'true'`),
		waitUntil(flat(`[data-mf-test]`)+` === 'Testing…'`),
		bothSchemes("agents-mcp-form-waiting"),
		chromedp.ActionFunc(func(context.Context) error { close(prober.gate); return nil }),
		waitUntil(visible(`[data-mf-result]`)+` && `+flat(`[data-mf-result-title]`)+` === 'Connected. 2 tools:'`),
		waitUntil(`[...document.querySelectorAll('[data-mf-tools] .tool')].map(t => t.textContent.trim()).join() === 'search_issues,create_issue' && !`+visible(`[data-mf-tools] span:not(.tool)`)),
		waitUntil(`!`+visible(`[data-mf-waiting]`)+` && document.querySelector('[data-mf-test]').disabled === false`),
		bothSchemes("agents-mcp-form-tested"),
	)
	asked := prober.asked()
	if len(asked) != 1 || asked[0].Command != "npx" || strings.Join(asked[0].Args, "|") != "-y|@linear/mcp-server|--label|two words" || asked[0].Env["SET_ONE"] != "1" {
		t.Fatalf("the server was asked to test %+v, want npx with the pasted arguments and SET_ONE expanded to 1", asked)
	}
	run(b, "Who gets it: an agent and a project are ticked, and the preview and the footer follow",
		mcpFormClick(`[data-mf-agent-row][data-name=backend] input`),
		mcpFormClick(`[data-mf-project-row][data-id="`+w.project.ID+`"] input`),
		waitUntil(flat(`[data-mf-reach]`)+` === 'Reaches 1 agent and 1 project, from their next session. Running sessions keep what they started with.'`),
		waitUntil(preview+` === 'agents.json,mcp-servers.json,project “harness”'`),
		waitUntil(flat(`[data-mcp-form] [data-preview-count]`)+` === '2 files · 1 project list'`),
		waitUntil(flat(`[data-mcp-form] [data-save-state]`)+`.endsWith('Reaches 1 agent and 1 project from their next session.')`),
	)
	run(b, "Add server writes it: the library lists it, the toast offers Undo",
		mcpFormClick(`[data-mcp-form] [data-save-footer] [data-save]`),
		waitUntil(`location.pathname === '/agents/mcp/linear'`),
		waitUntil(visible(`[data-mcp-detail]`)+` && document.querySelector('[data-mcp-detail] h2').textContent.trim() === 'linear'`),
		waitUntil(`!!document.querySelector('[data-mcp-row][data-key=linear]')`),
		waitUntil(visible(`[data-toast]`)+` && !!document.querySelector('[data-toast] [data-undo]')`),
		waitUntil(`[...document.querySelectorAll('[data-who=agents] a')].map(a => a.textContent.trim()).join() === 'backend'`),
	)
	servers := catalogFile("mcp-servers.json")
	for _, want := range []string{`"linear"`, `"command": "npx"`, `"@linear/mcp-server"`, `"two words"`, `"MCPFORM_E2E_LINEAR_KEY": "${MCPFORM_E2E_LINEAR_KEY}"`, `"SET_ONE": "${MCPFORM_E2E_SET}"`} {
		if !strings.Contains(servers, want) {
			t.Errorf("mcp-servers.json lacks %s:\n%s", want, servers)
		}
	}
	if !strings.Contains(catalogFile("agents.json"), `"backend": {`) || !strings.Contains(catalogFile("agents.json"), `"linear"`) {
		t.Errorf("agents.json does not give the backend agent the server:\n%s", catalogFile("agents.json"))
	}
	if !strings.Contains(projectServers(w.project.ID), "linear") || strings.Contains(projectServers(other.ID), "linear") {
		t.Errorf("project lists: harness %s, billing-api %s; want linear on the first only", projectServers(w.project.ID), projectServers(other.ID))
	}
	if strings.Contains(servers, mcpFormToken) || strings.Contains(wire.all(), mcpFormToken) {
		t.Errorf("the token reached the catalog or the wire")
	}
}

// TestMcpServerFormKeepsATokenAsTextOnlyWhenTold_EndToEnd is W1 for the same
// story's secret handling: a token pasted into a header is masked, blocks the
// preview, the test and the save, and travels nowhere; Keep as text writes it
// and says so, with the preview hiding it; opening the server again shows it
// masked and kept, and Use ${VAR} instead takes it out of the file. Errors are
// linked to their fields with aria-describedby.
func TestMcpServerFormKeepsATokenAsTextOnlyWhenTold_EndToEnd(t *testing.T) {
	prober := &mcpFormProber{gate: make(chan struct{})}
	close(prober.gate)
	w := newTokenSaverWorld(t, agentcatalog.WithProber(prober))
	writeCatalogFile(t, w, "agents.json", `{"agents": {"designer": {"description": "Designs screens."}}}
`)
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"command": "npx", "args": ["gh-mcp"]}}}
`)
	serversFile := func() string {
		raw, _ := os.ReadFile(filepath.Join(w.root, "agents", "mcp-servers.json"))
		return string(raw)
	}
	flat := func(sel string) string { return `(document.querySelector(` + quote(sel) + `)?.textContent ?? '').replace(/\s+/g, ' ').trim()` }
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	attr := func(sel, name string) string { return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)` }
	save := `document.querySelector('[data-mcp-form] [data-save-footer] [data-save]')`
	header := func(n int, field string) string {
		return fmt.Sprintf(`[data-mf-headers] > div:nth-of-type(%d) [data-field=%s]`, n, field)
	}
	previewText := `[...document.querySelectorAll('[data-mcp-form] [data-preview-file]')].map(f => f.textContent).join('\n')`
	run := func(b *browser, name string, actions ...chromedp.Action) {
		for i, a := range actions {
			b.run(fmt.Sprintf("%s [action %d]", name, i), a)
		}
	}
	wire := &mcpFormWire{}

	b := newTLSBrowser(t)
	run(b, "a name the catalog has is refused at once, at its field, with a link and a name to try",
		chromedp.EmulateViewport(1400, 1000),
		wire.watch(b),
		chromedp.Navigate(w.srv.URL+"/agents/mcp/new"),
		waitUntil(visible(`[data-mcp-form]`)),
		setField(`#mf-name`, "github"),
		waitUntil(visible(`#mf-name-error`)+` && `+flat(`#mf-name-error`)+`.startsWith('A server named github already exists.')`),
		waitUntil(attr(`#mf-name`, "aria-describedby")+` === 'mf-name-hint mf-name-error' && `+attr(`#mf-name`, "aria-invalid")+` === 'true'`),
		waitUntil(attr(`[data-open-taken]`, "href")+` === '/agents/mcp/github' && `+flat(`[data-use-suggestion]`)+` === 'github-2'`),
		mcpFormClick(`[data-use-suggestion]`),
		waitUntil(`document.querySelector('#mf-name').value === 'github-2' && !`+visible(`#mf-name-error`)),
		setField(`#mf-name`, "linear2"),
		mcpFormClick(`[data-transport=http]`),
		waitUntil(visible(`[data-mf-remote]`)+` && !`+visible(`[data-mf-local]`)),
		setField(`#mf-url`, "ftp://nope"),
		waitUntil(visible(`#mf-url-error`)+` && `+attr(`#mf-url`, "aria-describedby")+` === 'mf-url-hint mf-url-error'`),
		setField(`#mf-url`, "https://mcp.linear.app/mcp"),
		waitUntil(`!`+visible(`#mf-url-error`)),
	)
	run(b, "a token pasted into a header is masked, named, and blocks everything until the person chooses",
		mcpFormClick(`[data-mf-add][data-section=headers]`),
		waitUntil(`document.querySelectorAll('[data-mf-headers] [data-mf-row]').length === 1`),
		setField(header(1, "key"), "Authorization"),
		setField(header(1, "value"), "Bearer "+mcpFormToken),
		waitUntil(flat(`[data-mf-secret] strong`)+` === 'This looks like a Linear API key.'`),
		waitUntil(attr(header(1, "value"), "type")+` === 'password'`),
		waitUntil(`document.querySelector('[data-mf-headers] select[data-mf-mode]').value === 'text'`),
		waitUntil(save+`.disabled === true && document.querySelector('[data-mf-test]').disabled === true`),
		waitUntil(`document.querySelector('[data-preview-hint]').textContent.includes('No preview while a token waits')`),
		chromedp.Sleep(600*time.Millisecond),
		bothSchemes("agents-mcp-form-header-token"),
	)
	if got := wire.all(); strings.Contains(got, mcpFormToken) {
		t.Fatalf("the token was sent while its warning was open:\n%s", got)
	}
	run(b, "Keep as text says so, enables the save, and the preview hides the value",
		mcpFormClick(`[data-mf-keep]`),
		waitUntil(flat(`[data-mf-secret] strong`)+` === 'Kept as plain text.'`),
		waitUntil(flat(`[data-mf-secret]`)+`.includes('The key is written as it is into mcp-servers.json in the data directory.')`),
		waitUntil(`document.querySelector('[data-mf-use]').textContent.trim() === 'Use ${LINEAR2_API_KEY} instead'`),
		waitUntil(save+`.disabled === false`),
		waitUntil(previewText+`.includes('Authorization') && `+previewText+`.includes('••••••')`),
		waitUntil(`!`+previewText+`.includes('`+mcpFormToken+`')`),
		waitUntil(`!document.querySelector('main').innerText.includes('`+mcpFormToken+`')`),
		waitUntil(attr(header(1, "value"), "type")+` === 'password'`),
		mcpFormClick(`[data-mf-test]`),
		waitUntil(flat(`[data-mf-result-title]`)+` === 'Connected. 2 tools:'`),
	)
	run(b, "Add server writes the value as typed, once, in its own header",
		mcpFormClick(`[data-mcp-form] [data-save-footer] [data-save]`),
		waitUntil(`location.pathname === '/agents/mcp/linear2'`),
		waitUntil(visible(`[data-mcp-detail]`)),
		waitUntil(`!document.querySelector('main').innerText.includes('`+mcpFormToken+`')`),
		waitUntil(flat(`[data-mcp-detail] [data-entry]`)+`.includes('plain text, hidden')`),
	)
	if got := serversFile(); strings.Count(got, mcpFormToken) != 1 || !strings.Contains(got, `"Authorization": "Bearer `+mcpFormToken+`"`) {
		t.Fatalf("mcp-servers.json should hold the kept token once, in Authorization:\n%s", got)
	}
	run(b, "opening it again shows the header masked and kept, and Use ${VAR} instead takes the token out of the file",
		chromedp.Navigate(w.srv.URL+"/agents/mcp/linear2/edit"),
		waitUntil(visible(`[data-mcp-form]`)+` && document.querySelector('[data-mcp-form] h1').textContent.trim() === 'Edit linear2'`),
		waitUntil(attr(header(1, "value"), "type")+` === 'password' && `+flat(`[data-mf-secret] strong`)+` === 'Kept as plain text.'`),
		waitUntil(visible(`[data-mf-rename]`)+` && `+flat(`[data-mf-rename]`)+`.startsWith('No agent and no project has this server')`),
		waitUntil(save+`.disabled === true`),
		mcpFormClick(`[data-mf-use]`),
		waitUntil(`document.querySelector('`+header(1, "value")+`').value === 'Bearer ${LINEAR2_API_KEY}'`),
		waitUntil(save+`.disabled === false && `+save+`.textContent.trim() === 'Save server'`),
		mcpFormClick(`[data-mcp-form] [data-save-footer] [data-save]`),
		waitUntil(`location.pathname === '/agents/mcp/linear2'`),
		waitUntil(visible(`[data-mcp-detail]`)),
	)
	if got := serversFile(); strings.Contains(got, mcpFormToken) || !strings.Contains(got, `"Authorization": "Bearer ${LINEAR2_API_KEY}"`) {
		t.Fatalf("the token should be gone from mcp-servers.json:\n%s", got)
	}
}

// TestAgentEditorsNewMcpServerDrawerSavesTheServerAndKeepsTheEdit_EndToEnd is
// W1 for the drawer: from the agent editor, New MCP server… opens a side drawer
// over the half-done edit, the page under it cannot be reached, a taken name is
// refused at its field, Escape on a clean drawer closes it, and saving the
// server unattached ticks it in the editor with the edit still there; the agent
// gets the server when the agent is saved.
func TestAgentEditorsNewMcpServerDrawerSavesTheServerAndKeepsTheEdit_EndToEnd(t *testing.T) {
	prober := &mcpFormProber{gate: make(chan struct{})}
	close(prober.gate)
	w := newTokenSaverWorld(t, agentcatalog.WithProber(prober))
	writeCatalogFile(t, w, "agents.json", `{"agents": {"designer": {"description": "Designs screens.", "skills": ["tdd"]}}}
`)
	writeCatalogFile(t, w, "skills/tdd/SKILL.md", "---\nname: tdd\ndescription: Test first.\n---\nbody\n")
	writeCatalogFile(t, w, "mcp-servers.json", `{"mcpServers": {"github": {"command": "npx", "args": ["gh-mcp"]}}}
`)
	catalogFile := func(rel string) string {
		raw, _ := os.ReadFile(filepath.Join(w.root, "agents", rel))
		return string(raw)
	}
	visible := func(sel string) string { return `document.querySelector(` + quote(sel) + `)?.checkVisibility() === true` }
	attr := func(sel, name string) string { return `document.querySelector(` + quote(sel) + `)?.getAttribute(` + quote(name) + `)` }
	drawer := `[data-mcp-form-page][data-mode=drawer]`
	drawerSave := `document.querySelector('` + drawer + ` [data-save-footer] [data-save]')`
	run := func(b *browser, name string, actions ...chromedp.Action) {
		for i, a := range actions {
			b.run(fmt.Sprintf("%s [action %d]", name, i), a)
		}
	}

	b := newTLSBrowser(t)
	run(b, "New MCP server… opens a drawer over the agent editor, which keeps its half-done edit and becomes unreachable",
		chromedp.EmulateViewport(1400, 1000),
		chromedp.Navigate(w.srv.URL+"/agents/designer/edit"),
		waitUntil(visible(`[data-agent-editor]`)),
		setField(`#agent-desc`, "Designs screens, edited."),
		waitUntil(`document.querySelector('[data-agent-editor] [data-save-footer] [data-save]').disabled === false`),
		mcpFormClick(`[data-new-mcp]`),
		waitUntil(visible(drawer)+` && `+visible(drawer+` [data-mcp-form]`)),
		waitUntil(attr(drawer+` [data-mcp-form]`, "role")+` === 'dialog' && `+attr(drawer+` [data-mcp-form]`, "aria-modal")+` === 'true'`),
		waitUntil(`document.activeElement?.id === 'mf-name'`),
		waitUntil(`document.querySelector('[data-agent-editor]').inert === true`),
		waitUntil(`!`+visible(drawer+` [data-mf-who]`)),
		waitUntil(`document.querySelectorAll('[data-save-footer]').length === 1`),
		waitUntil(`location.pathname === '/agents/designer/edit'`),
		bothSchemes("agents-mcp-drawer"),
	)
	run(b, "a name the catalog has is refused at its field in the drawer too",
		setField(drawer+` #mf-name`, "github"),
		setField(drawer+` #mf-command`, "npx"),
		waitUntil(visible(`#mf-name-error`)+` && `+attr(`#mf-name`, "aria-describedby")+` === 'mf-name-hint mf-name-error'`),
		waitUntil(drawerSave+`.disabled === true`),
		waitUntil(`document.querySelectorAll('[data-agent-editor] [aria-invalid="true"]').length === 0`), // the drawer's errors are not the editor's
		bothSchemes("agents-mcp-drawer-name-taken"),
		mcpFormClick(`[data-use-suggestion]`),
		waitUntil(`document.querySelector('#mf-name').value === 'github-2'`),
		setField(`#mf-name`, ""),
	)
	run(b, "Cancel on a form with something in it asks first; Keep editing stays, Discard closes only the drawer",
		setField(`#mf-name`, "half-done"),
		mcpFormClick(drawer+` [data-save-footer] [data-cancel]`),
		waitUntil(visible(drawer+` [data-leave]`)),
		mcpFormClick(drawer+` [data-keep]`),
		waitUntil(`!`+visible(drawer+` [data-leave]`)+` && document.querySelector('#mf-name').value === 'half-done'`),
		mcpFormClick(drawer+` [data-save-footer] [data-cancel]`),
		waitUntil(visible(drawer+` [data-leave]`)),
		mcpFormClick(drawer+` [data-discard]`),
		waitUntil(`!`+visible(drawer)),
		waitUntil(`document.querySelector('#agent-desc').value === 'Designs screens, edited.' && document.querySelector('[data-agent-editor]').inert === false`),
		waitUntil(`document.activeElement?.dataset.newMcp !== undefined`),
	)
	run(b, "Escape closes an untouched drawer",
		mcpFormClick(`[data-new-mcp]`),
		waitUntil(visible(drawer)),
		pressKey("Escape", 27),
		waitUntil(`!`+visible(drawer)),
	)
	run(b, "Add server saves it alone, ticks it in the editor, and the edit is still there",
		mcpFormClick(`[data-new-mcp]`),
		waitUntil(visible(drawer)),
		setField(`#mf-name`, "playwright"),
		chromedp.Evaluate(`(() => { const el = document.querySelector('#mf-command'); el.focus();
			const dt = new DataTransfer(); dt.setData('text', 'npx @playwright/mcp@latest');
			el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true })); })()`, nil),
		waitUntil(`document.querySelector('#mf-command').value === 'npx'`),
		waitUntil(drawerSave+`.disabled === false && `+drawerSave+`.textContent.trim() === 'Add server'`),
		waitUntil(`document.querySelector('`+drawer+` [data-preview-files]').textContent.includes('playwright')`),
		waitUntil(`document.querySelectorAll('[data-mcp-form] [data-preview-file]').length === 1`),
		mcpFormClick(drawer+` [data-save-footer] [data-save]`),
		waitUntil(`!`+visible(drawer)),
		waitUntil(visible(`[data-agent-editor]`)+` && location.pathname === '/agents/designer/edit'`),
		waitUntil(`document.querySelector('#agent-desc').value === 'Designs screens, edited.'`),
		waitUntil(`document.querySelector('[data-agent-editor] [data-mcp-row][data-name=playwright] input').checked === true`),
		waitUntil(`document.querySelector('[data-agent-editor] [data-mcp-row][data-name=github] input').checked === false`),
		waitUntil(visible(`[data-toast]`)+` && !!document.querySelector('[data-toast] [data-undo]')`),
		waitUntil(`document.activeElement?.dataset.newMcp !== undefined`),
	)
	if strings.Contains(catalogFile("agents.json"), "playwright") {
		t.Fatalf("the server was written to the agent before the agent was saved:\n%s", catalogFile("agents.json"))
	}
	if got := catalogFile("mcp-servers.json"); !strings.Contains(got, `"playwright"`) || !strings.Contains(got, "@playwright/mcp@latest") {
		t.Fatalf("mcp-servers.json lacks the new server:\n%s", got)
	}
	run(b, "saving the agent gives it the server",
		waitUntil(`document.querySelector('[data-agent-editor] [data-save-footer] [data-save]').disabled === false`),
		mcpFormClick(`[data-agent-editor] [data-save-footer] [data-save]`),
		waitUntil(`location.pathname === '/agents/designer'`),
		waitUntil(visible(`[data-toast]`)),
	)
	raw := catalogFile("agents.json")
	var parsed struct {
		Agents map[string]struct {
			Description string   `json:"description"`
			MCPServers  []string `json:"mcp_servers"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	if d := parsed.Agents["designer"]; d.Description != "Designs screens, edited." || fmt.Sprint(d.MCPServers) != "[playwright]" {
		t.Fatalf("designer after saving = %+v, want the edited description and [playwright]:\n%s", d, raw)
	}
}
