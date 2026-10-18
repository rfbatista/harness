//go:build !windows

package web_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/domain"
)

// The Token savers page flags a saver that is on somewhere but not
// installed, so sessions that should use it run without it: caveman, on
// for one repository and never installed, and rtk once it is removed while
// a task has it on. An installed saver, or one on nowhere, is not flagged.
func TestTokenSaversPageFlagsASaverOnSomewhereButNotInstalled_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	rel := newFakeReleases()
	rel.serveRTK("v1.0.1", rtkArchive("rtk 1.0.1"), true)
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)

	h := newTokenSaversHarness(t, srv.URL)
	list := h.releases(strings.Repeat("0", 64))
	h.installBefore(list, domain.TokenSaverRTK)
	db, err := sqlite.Open(h.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	activations := sqlite.NewActivationRepository(db)
	for _, a := range []domain.Activation{
		{Saver: domain.TokenSaverRTK, Owner: domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: "t1"}, ProjectID: "p1", Value: domain.ActivationOn},
		{Saver: domain.TokenSaverCaveman, Owner: domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: "r1"}, ProjectID: "p1", Value: domain.ActivationOn},
		{Saver: domain.TokenSaverCodegraph, Owner: domain.ActivationOwner{Level: domain.ActivationLevelProject, ID: "p1"}, ProjectID: "p1", Value: domain.ActivationOff},
	} {
		if err := activations.Put(a); err != nil {
			t.Fatal(err)
		}
	}
	page := h.serve(assets, list, darwinARM64)

	flagged := func(saver string) string {
		return `!!` + row(saver) + `?.querySelector('[data-active-not-installed]')`
	}
	b := newBrowser(t)
	b.run("caveman is flagged; rtk, installed, and codegraph, on nowhere, are not",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
		waitUntil(flagged("caveman")+` && `+rowText("caveman")+`.includes('on for some sessions but not installed')`),
		waitUntil(`!`+flagged("rtk")+` && !`+flagged("codegraph")),
	)
	b.run("rtk is flagged once removed, also after a reload",
		clickButton(row("rtk"), "Remove"),
		waitUntil(stateOf("rtk")+` === 'not_installed' && `+flagged("rtk")),
		chromedp.Reload(),
		waitUntil(savers+`.length === 3 && `+flagged("rtk")+` && `+flagged("caveman")+` && !`+flagged("codegraph")),
	)
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
