//go:build !windows

package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
)

// The person reviews an instruction-file proposal in the Instructions & rules
// workspace, against the real server: the proposal endpoints, the project feed
// and the pages' first count, with a headless Chrome as the person. The same
// flow runs against a Go fake of the contract in internal/adapter/in/web
// (browser_architecture_instruction_proposals_test.go).

// proposalPages serves the pages beside the shell e2e's API, so one browser
// can open the workspace of the repository the server holds.
func (e *shellE2E) proposalPages() *httptest.Server {
	e.t.Helper()
	assets, err := web.NewAssets()
	if err != nil {
		e.t.Fatal(err)
	}
	if !assets.Built() {
		e.t.Skip("browser test: web client not built; run `make web`")
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, TaskResources: e.plan, TaskComments: e.plan, Artifacts: e.art, Shells: e.shells,
		RepositoryArchitectures: e.cat.RepositoryArchitectures, InstructionProposals: e.cat.InstructionProposals, InstructionProposalDecisions: e.cat.InstructionProposals,
		Projects: e.cat.Projects, Agents: e.cat.Agents, Notifications: e.notes, Settings: e.cat.Settings,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: e.cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: e.cat.Agents, Repositories: e.cat.Projects, EnvFiles: e.cat.Projects,
		OpenInstructionProposals: openProposals(e.cat.InstructionProposals),
	}, assets, nil))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true // each page holds an event stream open
	srv.StartTLS()
	e.t.Cleanup(func() {
		srv.CloseClientConnections() // the pages' event streams never end on their own
		srv.Close()
	})
	return srv
}

const (
	realPanel   = `#architecture-panel-instructions`
	realPane    = realPanel + ` [data-proposal-pane]`
	realBadge   = `(document.querySelector('#architecture-tab-instructions [data-proposals-count]')?.textContent.trim() ?? '')`
	realFeature = skillPrefix + "features-harness/SKILL.md"
)

func TestInstructionProposalsInTheWebUI_ApproveAgainstTheRealServer(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	e := newShellE2E(t)
	_, got := e.submitted()
	srv := e.proposalPages()
	person := newPerson(t)

	person.run(t, "open the workspace",
		chromedp.EmulateViewport(1400, 1000),
		chromedp.Navigate(srv.URL+"/projects/"+e.project+"/repositories/"+e.repo+"/architecture/instructions"),
		waitFor(`document.querySelectorAll('`+realPanel+` [data-proposal]').length === 1`),
		waitFor(realBadge+` === '1'`),
	)
	if row := person.text(t, `document.querySelector('`+realPanel+` [data-proposal]').textContent.replace(/\s+/g, ' ')`); !strings.Contains(row, "Verification skill and its feature map") || !strings.Contains(row, "4 files") || !strings.Contains(row, "verify-harness") {
		t.Fatalf("row = %q", row)
	}

	person.run(t, "open the proposal",
		chromedp.Evaluate(`document.querySelector('`+realPanel+` [data-open-proposal]').click()`, nil),
		waitFor(`document.querySelectorAll('`+realPane+` .diff-view .file').length === 4`),
	)
	if ops := person.text(t, `[...document.querySelectorAll('`+realPane+` .diff-view .file')].map(f => f.dataset.op).sort().join()`); ops != "change,create,create,remove" {
		t.Fatalf("ops = %q", ops)
	}

	person.run(t, "approve",
		chromedp.Evaluate(`document.querySelector('`+realPane+` [data-approve]').click()`, nil),
		waitFor(`!!document.querySelector('`+realPane+` [data-approved-notice]')`),
		waitFor(realBadge+` === ''`),
		// The files list follows the saves the server announced.
		waitFor(`!!document.querySelector('`+realPanel+` [data-open-file][aria-label="Open `+skillPrefix+`verify-harness/launch.md"]')`),
		waitFor(`!document.querySelector('`+realPanel+` [data-open-file][aria-label="Open `+skillPrefix+`features-harness/old-feature.md"]')`),
	)
	if rows, open := e.listProposals(""); len(rows) != 1 || rows[0].ID != got.ProposalID || rows[0].State != "approved" || open != 0 {
		t.Fatalf("after approve: %+v open %d", rows, open)
	}
	if len(person.errors) > 0 {
		t.Fatalf("JS errors: %v", person.errors)
	}
}

func TestInstructionProposalsInTheWebUI_AStaleFileRefusesTheApprovalAndSendBackKeepsTheNote(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	e := newShellE2E(t)
	_, got := e.submitted()
	srv := e.proposalPages()
	person := newPerson(t)

	person.run(t, "open the proposal from its link",
		chromedp.EmulateViewport(1400, 1000),
		chromedp.Navigate(srv.URL+"/projects/"+e.project+"/repositories/"+e.repo+"/architecture/instructions?proposal="+got.ProposalID),
		waitFor(`document.querySelectorAll('`+realPane+` .diff-view .file').length === 4`),
	)
	// The person saves a file the proposal changes; the pane marks it stale at
	// once. An event sent before the page's feed is connected is lost, so the
	// person keeps saving (each save is a new version) until the page has heard one.
	version := 1
	var marked bool
	for attempt := 0; attempt < 40 && !marked; attempt++ {
		if _, err := e.cat.RepositoryArchitectures.SaveInstructionFile(e.ctx, e.repo, realFeature, "skill", version, featuresSkill+"The person's edit.\n"); err != nil {
			t.Fatal(err)
		}
		version++
		time.Sleep(250 * time.Millisecond)
		person.run(t, "look for the stale mark", chromedp.Evaluate(`document.querySelector('`+realPane+` .file[data-path="`+realFeature+`"]')?.matches('[data-stale="true"]') === true`, &marked))
	}
	person.run(t, "the stale mark",
		waitFor(`document.querySelector('`+realPane+` .file[data-path="`+realFeature+`"]')?.matches('[data-stale="true"]') === true`),
		waitFor(`!!document.querySelector('`+realPane+` [data-stale-notice]')`),
		waitFor(`document.querySelector('`+realPanel+` [data-stale-files]')?.textContent.trim() === '1 stale file'`),
	)
	person.run(t, "approve is refused",
		chromedp.Evaluate(`document.querySelector('`+realPane+` [data-approve]').click()`, nil),
		waitFor(`!!document.querySelector('`+realPane+` [data-refusal]')`),
	)
	if paths := person.text(t, `[...document.querySelectorAll('`+realPane+` [data-refusal] .refused-paths li')].map(li => li.textContent.replace(/\s+/g, ' ').trim()).join('|')`); !strings.HasPrefix(paths, realFeature) {
		t.Fatalf("refused paths = %q", paths)
	}
	if rows, _ := e.listProposals(""); rows[0].State != "open" {
		t.Fatalf("a refused approval changed the proposal: %+v", rows[0])
	}

	person.run(t, "send it back with a note",
		chromedp.Evaluate(`document.querySelector('`+realPane+` [data-open-sendback]').click()`, nil),
		waitFor(`!!document.querySelector('`+realPane+` [data-ask] textarea')`),
		chromedp.Evaluate(`(() => { const n = document.querySelector('`+realPane+` [data-ask] textarea'); n.value = 'Re-read features-harness and submit again.'; n.dispatchEvent(new Event('input', { bubbles: true })); })()`, nil),
		chromedp.Evaluate(`document.querySelector('`+realPane+` [data-ask] [type=submit]').click()`, nil),
		waitFor(`document.querySelector('`+realPanel+` [data-proposal]')?.dataset.state === 'sent_back'`),
		waitFor(realBadge+` === ''`),
	)
	rows, open := e.listProposals("")
	if rows[0].State != "sent_back" || rows[0].Note == nil || *rows[0].Note != "Re-read features-harness and submit again." || open != 0 {
		t.Fatalf("after send back: %+v open %d", rows[0], open)
	}
}
