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

// The Token savers page flags a saver that would reach a session but is
// not installed, so sessions that should use it run without it: caveman,
// on for one repository and never installed, and rtk once it is removed
// while a task has it on. An installed saver, or one that reaches no
// session, is not flagged: codegraph is on for a project whose only
// repository turns it off.
func TestTokenSaversPageFlagsASaverOnSomewhereButNotInstalled_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	rel := newFakeReleases()
	rel.serveRTK("v1.0.1", rtkArchive("rtk 1.0.1"), true)
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)

	h := newTokenSaversHarness(t, srv.URL)
	list := h.releases(strings.Repeat("0", 64))
	h.installBefore(list, domain.TokenSaverRTK)
	cat := h.service(list, darwinARM64)
	ctx := t.Context()
	api, err := cat.Projects.CreateProject(ctx, "api", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server, err := cat.Projects.CreateRepository(ctx, api.ID, "server", "", "https://example.com/server.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Projects.CreateRepository(ctx, api.ID, "client", "", "https://example.com/client.git", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	docs, err := cat.Projects.CreateProject(ctx, "docs", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site, err := cat.Projects.CreateRepository(ctx, docs.ID, "site", "", "https://example.com/site.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(h.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	activations := sqlite.NewActivationRepository(db)
	for _, a := range []domain.Activation{
		{Saver: domain.TokenSaverRTK, Owner: domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: "t1"}, ProjectID: api.ID, Value: domain.ActivationOn},
		{Saver: domain.TokenSaverCaveman, Owner: domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: server.ID}, ProjectID: api.ID, Value: domain.ActivationOn},
		{Saver: domain.TokenSaverCodegraph, Owner: domain.ActivationOwner{Level: domain.ActivationLevelProject, ID: docs.ID}, ProjectID: docs.ID, Value: domain.ActivationOn},
		{Saver: domain.TokenSaverCodegraph, Owner: domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: site.ID}, ProjectID: docs.ID, Value: domain.ActivationOff},
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
	b.run("caveman is flagged; rtk, installed, and codegraph, reaching no session, are not",
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
