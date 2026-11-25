//go:build !windows

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

// The digest time and the Digests tab in the browser, against the real server with its
// controllable digest clock (ADR 0113, ADR 0115).

// newPODigestWeb is the product owner's web UI in front of a server whose digest scheduler reads
// the returned clock and ticks every few milliseconds.
func newPODigestWeb(t *testing.T) (*poWeb, *digestClock) {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test: skipped in -short mode")
	}
	assets, err := web.NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	e, clk := newDigestE2E(t)
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, Projects: e.cat.Projects, Settings: e.cat.Settings, ProductOwner: e.po, AgentBoard: e.board, Usage: e.usage,
	})))
	mux.Handle("/", web.NewHandler(web.Deps{
		Projects: e.cat.Projects, Tasks: e.plan, Sessions: e.orch, Agents: e.cat.Agents, Repositories: e.cat.Projects,
		EnvFiles: e.cat.Projects, Documents: e.plan, AgentBoard: e.board,
	}, assets, nil))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections() // the page's event stream never ends on its own
		srv.Close()
	})
	return &poWeb{poE2E: e, url: srv.URL}, clk
}

const (
	digestInput   = `document.querySelector('#digest-time')`
	digestNote    = `(document.querySelector('#digest-time-note')?.textContent ?? '')`
	dateRows      = `[...document.querySelectorAll('.digests .dates .date-row')]`
	newestDateRow = `document.querySelector('.digests .dates a.date-row')`
	digestHead    = `(document.querySelector('.digests .dates-head')?.textContent ?? '')`
)

// typeTime is what a person does to a native time input: the browser fires input and change once the
// time is complete.
func typeTime(hhmm string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(() => {
		const input = %s;
		Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, %q);
		input.dispatchEvent(new Event('input', { bubbles: true }));
		input.dispatchEvent(new Event('change', { bubbles: true }));
	})()`, digestInput, hhmm), nil)
}

func TestTheDigestTimeAndTheDigestsTabFollowTheServerInTheWebUI_EndToEnd(t *testing.T) {
	w, clk := newPODigestWeb(t)
	p := newPerson(t)
	zone := w.settings().TimeZone
	if zone == "" {
		t.Fatal("the server names no time zone")
	}
	project := w.url + "/projects/" + w.project

	p.run(t, "the person opens the Settings page: no digest time, the server's zone, the hint",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(project+"/settings"),
		waitFor(`!!`+digestInput+` && !`+digestInput+`.disabled`),
	)
	if got := p.text(t, digestNote); !strings.Contains(got, zone+", the server's time zone") || !strings.Contains(got, "no digest is recorded until a time is set") {
		t.Errorf("the note says %q, want the zone and that nothing is recorded", got)
	}
	if got := p.text(t, `document.querySelector('#digest-time-hint').textContent`); !strings.Contains(got, "A quiet day records nothing") || !strings.Contains(got, "while the harness was down") {
		t.Errorf("the hint says %q, want quiet days and downtime", got)
	}
	if p.truth(t, `[...document.querySelectorAll('#product-owner button')].some(b => b.textContent.trim() === 'Clear time')`) {
		t.Error("Clear time shows with no time set")
	}

	p.run(t, "a time typed in the native input is saved at once, and the next run follows",
		typeTime("08:30"),
		waitFor(`/next: \w{3} \d+ \w{3}, 08:30$/.test(`+digestNote+`)`),
		waitFor(`[...document.querySelectorAll('#product-owner button')].some(b => b.textContent.trim() === 'Clear time')`),
	)
	if got := w.settings().DigestTime; got == nil || *got != "08:30" {
		t.Errorf("the server holds the digest time %v, want 08:30", got)
	}

	p.run(t, "Browse digests opens the Digests tab: empty, saying when the first digest covers",
		chromedp.Click(`#digest-time-hint a`, chromedp.ByQuery),
		waitFor(`location.pathname === '/projects/`+w.project+`/product-owner' && location.hash === '#digests'`),
		waitFor(`document.querySelector('[role=tab][aria-selected=true]')?.textContent.trim() === 'Digests'`),
		waitFor(`!!document.querySelector('.digest-empty')`),
	)
	if got := p.text(t, `document.querySelector('.digest-empty').textContent`); !strings.Contains(got, "No digests yet") || !strings.Contains(got, "covers the period from when the time was set") || !strings.Contains(got, "08:30") {
		t.Errorf("the empty state says %q", got)
	}

	// The clock passes the digest time with a task that moved: the digest shows up on the open tab.
	moved := w.moveTask("Product owner digest", domain.TicketStatusReview, clk.at(1, 6, 0))
	clk.set(clk.at(1, 8, 31))
	p.run(t, "the digest recorded at the set time appears live, with its section and a working link",
		waitFor(`!document.querySelector('.digest-empty') && `+dateRows+`.length === 1`),
		waitFor(`!!document.querySelector('.digest h2') && document.querySelector('.digest h2').textContent === 'Wednesday 2 January'`),
	)
	if got := p.text(t, `[...document.querySelectorAll('.digest section > h3')].map(h => h.textContent.trim()).join('|')`); got != "Tasks that moved 1" {
		t.Errorf("the digest's sections = %q, want Tasks that moved", got)
	}
	taskLink := `document.querySelector('.digest section li a')`
	if got := p.text(t, taskLink+`.getAttribute('href')`); got != "/projects/"+w.project+"/tasks/"+moved.ID {
		t.Errorf("the item links to %q, want the task's page", got)
	}
	if got := p.text(t, `document.querySelector('.digest section li .move').textContent`); !strings.Contains(got, "Review") {
		t.Errorf("the move reads %q, want it to end at Review", got)
	}
	if got := p.text(t, digestHead); !strings.Contains(got, "Next:") || !strings.Contains(got, "08:30") {
		t.Errorf("the date column's head says %q, want the next run at 08:30", got)
	}

	// Two quiet days, then another digest: the gap shows as one grey row, the digest read stays on screen.
	w.moveTask("A later move", domain.TicketStatusDone, clk.at(3, 7, 0))
	clk.set(clk.at(3, 9, 0))
	p.run(t, "a later digest joins at the top, a quiet stretch is one grey row, and the digest being read stays",
		waitFor(dateRows+`.length === 3`),
		waitFor(dateRows+`[1].hasAttribute('data-gap')`),
	)
	if got := p.text(t, dateRows+`[1].textContent`); !strings.Contains(got, "Thu 3 – Fri 4") || !strings.Contains(got, "nothing recorded") {
		t.Errorf("the gap row says %q, want the two quiet days", got)
	}
	if got := p.text(t, `document.querySelector('.digest h2').textContent`); got != "Wednesday 2 January" {
		t.Errorf("the digest on screen is %q, want the one the person was reading", got)
	}
	p.run(t, "choosing the newer date shows its digest",
		chromedp.Click(newestDateRow, chromedp.ByJSPath),
		waitFor(`document.querySelector('.digest h2').textContent === 'Saturday 5 January'`),
		waitFor(`location.hash === '#digests/2030-01-05'`),
	)

	p.run(t, "the product owner off: the tab says no digest is recorded and keeps the digests readable",
		chromedp.Navigate(project+"/settings"),
		waitFor(`!!`+digestInput),
		chromedp.Click(`#product-owner [role=switch]`, chromedp.ByQuery),
		waitFor(`/paused while the product owner is off/.test(`+digestNote+`) && !!document.querySelector('.digest-time[data-dimmed]')`),
	)
	p.run(t, "the Digests tab keeps what was recorded and says why nothing more will be",
		chromedp.Navigate(project+"/product-owner#digests"),
		waitFor(`!!document.querySelector('.digests .banner')`),
	)
	if got := p.text(t, `document.querySelector('.digests .banner').textContent`); !strings.Contains(got, "The product owner is off, so no digest is recorded. The 2 digests below are kept.") {
		t.Errorf("the banner says %q", got)
	}
	if got := p.text(t, digestHead); !strings.Contains(got, "Paused while the product owner is off") {
		t.Errorf("the date column's head says %q, want it paused", got)
	}
	p.run(t, "Turn it on brings the banner down",
		chromedp.Click(`.digests .banner .button`, chromedp.ByQuery),
		waitFor(`!document.querySelector('.digests .banner') && `+digestHead+`.includes('Next:')`),
	)
	if !w.settings().Enabled {
		t.Error("the server has the product owner off after Turn it on")
	}

	p.run(t, "clearing the time on the Settings page records nothing more, and the tab says there is no time",
		chromedp.Navigate(project+"/settings"),
		waitFor(`!!`+digestInput+` && `+digestInput+`.value === '08:30'`),
		chromedp.Click(`#product-owner .digest-time .button[data-size=sm]`, chromedp.ByQuery),
		waitFor(`/no digest is recorded until a time is set/.test(`+digestNote+`) && `+digestInput+`.value === ''`),
	)
	if got := w.settings().DigestTime; got != nil {
		t.Errorf("the server still holds the digest time %v", *got)
	}
	p.run(t, "the Digests tab shows the same",
		chromedp.Navigate(project+"/product-owner#digests"),
		waitFor(digestHead+`.includes('No digest time set')`),
	)
}

func TestDigestsPageThirtyAtATimeInTheDigestsTab_EndToEnd(t *testing.T) {
	w, _ := newPODigestWeb(t)
	db, err := sqlite.Open(w.cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewProductOwnerDigestRepository(db)
	// Thirty-two days of digests, seeded straight into the store, as a digest run per day would take minutes.
	for day := 1; day <= 32; day++ {
		date := time.Date(2029, 12, day, 0, 0, 0, 0, time.Local)
		d := domain.ProductOwnerDigest{ID: fmt.Sprintf("d%02d", day), ProjectID: w.project, Date: date.Format("2006-01-02"),
			PeriodStart: date, PeriodEnd: date.Add(9 * time.Hour), Summary: fmt.Sprintf("Summary of day %d.", day),
			Items: []domain.ProductOwnerDigestItem{{Kind: domain.DigestItemTaskMoved, Title: "t", URL: "/projects/" + w.project}}}
		if err := repo.SaveDigest(w.ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	p := newPerson(t)

	p.run(t, "the Digests tab opens on the newest 30, at the address's date when it names one",
		chromedp.EmulateViewport(1440, 900),
		chromedp.Navigate(w.url+"/projects/"+w.project+"/product-owner#digests/2029-12-20"),
		waitFor(dateRows+`.length >= 30`),
		waitFor(`document.querySelector('.digest h2')?.textContent === 'Thursday 20 December'`),
	)
	if got := p.text(t, dateRows+`.length + '|' + `+dateRows+`[0].querySelector('.d').textContent`); got != "30|Tue 1 Jan" {
		t.Errorf("rows|first = %q, want 30 rows from Tue 1 Jan", got)
	}

	p.run(t, "Load earlier digests adds the last two, without a repeat, and goes away",
		chromedp.Click(`.digests .earlier .button`, chromedp.ByQuery),
		waitFor(dateRows+`.length === 32 && !document.querySelector('.digests .earlier')`),
	)
	if got := p.text(t, `new Set(`+dateRows+`.map(r => r.querySelector('.d').textContent)).size + ''`); got != "32" {
		t.Errorf("distinct dates = %s, want 32", got)
	}
	if p.truth(t, dateRows+`.some(r => r.hasAttribute('data-gap'))`) {
		t.Error("a gap row shows between consecutive dates")
	}
}
