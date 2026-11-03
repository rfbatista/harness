//go:build !windows

package web_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// raAPI calls the server as another person would: a GET or a POST with a JSON body.
func raAPI(t *testing.T, w *tokenSaverWorld, method, path, body string) map[string]any {
	t.Helper()
	var res interface {
		Close() error
	}
	var raw []byte
	var status int
	if method == "GET" {
		r, err := w.srv.Client().Get(w.srv.URL + "/api/" + path)
		if err != nil {
			t.Fatal(err)
		}
		res, status = r.Body, r.StatusCode
		raw, _ = io.ReadAll(r.Body)
	} else {
		r, err := w.srv.Client().Post(w.srv.URL+"/api/"+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, status = r.Body, r.StatusCode
		raw, _ = io.ReadAll(r.Body)
	}
	_ = res.Close()
	if status != 200 {
		t.Fatalf("%s %s: %d %s", method, path, status, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

const (
	raRow       = `[data-agent-row][data-agent="%s"]`
	raAssigned  = `document.querySelectorAll('[data-group=assigned] [data-agent-row]').length`
	raNotAssign = `document.querySelectorAll('[data-group=not-assigned] [data-agent-row]').length`
	raToast     = `(document.querySelector('[data-toast]')?.innerText ?? '').replace(/\s+/g, ' ').trim()`
	raLive      = `document.querySelector('.stream-bar [data-state="live"]') !== null`
)

func raClick(sel string) chromedp.Action {
	return waitUntil(`(() => { const b = document.querySelector(` + quote(sel) + `); if (!b || b.disabled) return false; b.click(); return true; })()`)
}

// TestAgentsByRepositoryJourney_EndToEnd is the whole journey of the spec
// "Web UI — Manage agents by repository", against a real server: unassign
// with Undo, assign again, write a prompt and see the server's preview, hit a
// stale-save banner, then open the new-session picker and see the missing
// agent greyed with its reason.
func TestAgentsByRepositoryJourney_EndToEnd(t *testing.T) {
	w := newTokenSaverWorld(t)
	writeCatalogFile(t, w, "agents.json", nsAgentsCatalog)
	nsAddCatalogAgents(t, w)
	b := newTLSBrowser(t)
	tab := w.srv.URL + "/projects/" + w.project.ID + "/repositories/" + w.repo.ID + "/architecture/agents"
	reviewer := strings.ReplaceAll(raRow, "%s", "reviewer")
	backend := strings.ReplaceAll(raRow, "%s", "backend")

	var toast string
	b.run("the Agents tab lists every enabled agent as assigned",
		chromedp.EmulateViewport(1280, 1000),
		chromedp.Navigate(tab),
		waitUntil(raAssigned+` >= 2`),
		chromedp.Sleep(time.Second), // the page feed follows from init; give it a moment before acting
		bothSchemes("repo-agents-tab"),
	)

	b.run("Unassign moves the agent at once and the toast offers Undo",
		raClick(reviewer+` [data-action=unassign]`),
		waitUntil(raNotAssign+` === 1`),
		chromedp.Evaluate(raToast, &toast),
		bothSchemes("repo-agents-unassigned"),
	)
	if !strings.Contains(toast, "Unassigned reviewer from api.") || !strings.Contains(toast, "It stays available in other repositories.") {
		t.Errorf("toast = %q", toast)
	}

	b.run("Undo puts it back",
		raClick(`[data-undo]`),
		waitUntil(raNotAssign+` === 0`),
		chromedp.Evaluate(raToast, &toast),
	)
	if !strings.Contains(toast, "reviewer is assigned to api again.") {
		t.Errorf("toast after Undo = %q", toast)
	}

	b.run("Assign again from the row, after a second Unassign",
		raClick(reviewer+` [data-action=unassign]`),
		waitUntil(raNotAssign+` === 1`),
		raClick(`[data-group=not-assigned] [data-action=assign]`),
		waitUntil(raNotAssign+` === 0`),
	)

	var section, own string
	b.run("a repository prompt: write it, and the preview is the server's text",
		raClick(backend+` [data-action=prompt]`),
		waitUntil(`!!document.querySelector('[data-prompt-editor]')?.checkVisibility()`),
		waitUntil(`document.querySelector('#repository-prompt-text') && !document.querySelector('#repository-prompt-text').disabled`),
		setField(`#repository-prompt-text`, "Run make check before every commit."),
		raClick(`#architecture-panel-agents [data-view=preview]`),
		waitUntil(`!!document.querySelector('[data-preview-section]')`),
		chromedp.Evaluate(`document.querySelector('[data-preview-section]').textContent`, &section),
		chromedp.Evaluate(`document.querySelector('[data-preview-own] pre').textContent`, &own),
		bothSchemes("repo-prompt-preview"),
	)
	if !strings.HasPrefix(section, "## Repository prompt: api") || !strings.Contains(section, "Run make check before every commit.") {
		t.Errorf("preview section = %q", section)
	}
	_ = own // the agent's own prompt is the server's (empty for an agent with no definition body)

	b.run("Save stores it",
		raClick(`#architecture-panel-agents [data-view=write]`),
		raClick(`[data-action=save-prompt]`),
		waitUntil(`document.querySelector('[data-bar-note]').textContent.trim() === 'No changes'`),
	)
	saved := raAPI(t, w, "GET", "get_repository_prompt?repository_id="+w.repo.ID+"&agent=backend", "")
	if saved["content"] != "Run make check before every commit." {
		t.Fatalf("saved prompt = %v", saved)
	}

	// Another person saves the same prompt; this tab's next save is stale.
	raAPI(t, w, "POST", "save_repository_prompt", `{"repository_id":"`+w.repo.ID+`","agent":"backend","content":"Theirs.","version":"`+saved["version"].(string)+`"}`)
	var draft string
	b.run("a stale save keeps the draft and shows the conflict banner",
		setField(`#repository-prompt-text`, "Mine, edited."),
		raClick(`[data-action=save-prompt]`),
		waitUntil(`!!document.querySelector('[data-prompt-conflict]')`),
		chromedp.Evaluate(`document.querySelector('#repository-prompt-text').value`, &draft),
		bothSchemes("repo-prompt-stale"),
	)
	if draft != "Mine, edited." {
		t.Errorf("draft after the stale save = %q", draft)
	}

	// The reviewer is unassigned by someone else; the picker greys it with its reason.
	tabState := raAPI(t, w, "GET", "get_repository_agents?repository_id="+w.repo.ID, "")
	raAPI(t, w, "POST", "unassign_agent", `{"repository_id":"`+w.repo.ID+`","agent":"reviewer","list_version":"`+tabState["list_version"].(string)+`"}`)
	var hint, detail string
	var missing bool
	b.run("the agent detail lists the repository with Assign again",
		chromedp.Navigate(w.srv.URL+"/agents/reviewer"),
		waitUntil(`document.querySelectorAll('[data-repository-row]').length >= 1`),
		chromedp.Evaluate(`document.querySelector('[data-repository-row]').innerText.replace(/\s+/g, ' ').trim()`, &detail),
		bothSchemes("agent-detail-repositories"),
	)
	if !strings.Contains(detail, "api") || !strings.Contains(detail, "Unassigned") || !strings.Contains(detail, "Assign again") {
		t.Errorf("repository row on the detail = %q", detail)
	}
	b.run("Assign again from the detail works, and Unassign takes it away once more",
		raClick(`[data-action=assign-repository]`),
		// assigned everywhere: the repositories collapse under a summary
		waitUntil(`document.querySelectorAll('[data-repository-row]').length === 0 && document.querySelector('[data-action=toggle-repositories]')?.textContent.includes('all assigned')`),
		raClick(`[data-action=toggle-repositories]`),
		waitUntil(`!!document.querySelector('[data-repository-row] [data-action=unassign-repository]')`),
		raClick(`[data-action=unassign-repository]`),
		waitUntil(`!!document.querySelector('[data-repository-row] [data-action=assign-repository]')`),
	)
	b.run("the new-session picker offers only the assigned agents and explains the missing one",
		chromedp.Navigate(w.taskURL()),
		nsClick(`main header`, "New session"),
		waitUntil(`document.querySelectorAll('#new-session-agent option').length > 1`),
		waitUntil(raLive),
		waitUntil(`!!document.querySelector('[data-not-offered]')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#new-session-agent option')].map(o => o.textContent.trim()).includes('reviewer')`, &missing),
		chromedp.Evaluate(`document.querySelector('[data-not-offered]').open = true`, nil),
		chromedp.Evaluate(`document.querySelector('[data-not-offered]').innerText.replace(/\s+/g, ' ').trim()`, &hint),
		bothSchemes("new-session-picker-unassigned"),
	)
	if missing {
		t.Errorf("reviewer is still offered")
	}
	if !strings.Contains(hint, "reviewer — unassigned from api") {
		t.Errorf("not offered = %q", hint)
	}
}
