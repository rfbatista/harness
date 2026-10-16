package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const browserSecret = "ghp_browserSecretNeverShown4242"

// quietProjects is a project catalog feed with no changes: the credentials'
// own changes ride the same stream.
type quietProjects struct{}

func (quietProjects) FollowProjects(ctx context.Context) (<-chan ports.ProjectCatalogChange, error) {
	ch := make(chan ports.ProjectCatalogChange)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

type credentialsHarness struct {
	srv       *httptest.Server
	fake      *gitcredentialstest.Fake
	projectID string
	repos     map[string]string // name → id
}

// newCredentialsHarness serves the pages over the server's in-memory git
// credentials and the real /api router, with one project of three
// repositories (https, ssh, no origin).
func newCredentialsHarness(t *testing.T) *credentialsHarness {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	fake := gitcredentialstest.New()
	h := &credentialsHarness{fake: fake, repos: map[string]string{}}
	var id string
	h.projectID, id = fake.AddRepository("", "harness", "https://github.com/acme/harness.git")
	h.repos["harness"] = id
	_, h.repos["infra"] = fake.AddRepository(h.projectID, "infra", "git@github.com:acme/infra.git")
	_, h.repos["scratch"] = fake.AddRepository(h.projectID, "scratch", "")
	var repos []*domain.Repository
	for _, name := range []string{"harness", "infra", "scratch"} {
		repos = append(repos, &domain.Repository{ID: h.repos[name], ProjectID: h.projectID, Name: name, RootDir: "/src/" + name})
	}
	pages := NewHandler(Deps{
		Projects:       fakeProjects{[]*domain.Project{{ID: h.projectID, Name: "acme", RootDir: "/src"}}},
		Tasks:          fakeTickets{},
		Sessions:       fakeSessions{},
		Agents:         fakeAgents{},
		Repositories:   fakeRepos{repos},
		GitCredentials: fake,
	}, assets, nil)
	api := httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{GitCredentials: fake, ProjectFeed: quietProjects{}}))
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/", pages)
	h.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		h.srv.CloseClientConnections()
		h.srv.Close()
	})
	return h
}

// credPoll waits for js; a timeout says which wait it was and what main shows.
func credPoll(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		err := chromedp.Poll(js, nil, chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)).Do(ctx)
		if err == nil {
			return nil
		}
		var shown string
		_ = chromedp.Evaluate(`document.querySelector('main')?.innerText.replace(/\s+/g, ' ').slice(0, 600)`, &shown).Do(ctx)
		return fmt.Errorf("waiting for %s: %w\nmain shows: %s", js, err, shown)
	})
}

const (
	credPageLive = `!document.querySelector('main [data-ssr]')`
	credNames    = `[...document.querySelectorAll('main .credential-row .title')].filter(e => e.checkVisibility()).map(e => e.textContent.trim()).join(',')`
	// noSecret is false when the token is anywhere a page keeps things.
	noSecret = `!(document.documentElement.outerHTML.includes('` + browserSecret + `') ||
		[...document.querySelectorAll('input, textarea')].some(i => i.value.includes('` + browserSecret + `')) ||
		JSON.stringify(Object.assign({}, localStorage, sessionStorage)).includes('` + browserSecret + `'))`
)

func TestGitCredentialsAddedInTheBrowserNeverLeaveTheirSecretBehind(t *testing.T) {
	h := newCredentialsHarness(t)
	ctx, errs := browser(t)
	other, _ := browser(t) // a second person, in their own browser

	if err := chromedp.Run(other, chromedp.Navigate(h.srv.URL+"/git-credentials"), credPoll(credPageLive)); err != nil {
		t.Fatal(err)
	}
	var names string
	var clean bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/git-credentials"),
		credPoll(credPageLive),
		credPoll(`!!document.querySelector('main').textContent.includes('No git credentials yet')`),
		clickButton(`main header`, "Add credential"),
		credPoll(`document.querySelector('#credential-name').checkVisibility()`),
		chromedp.SendKeys(`#credential-name`, "work-token", chromedp.ByQuery),
		chromedp.SendKeys(`#credential-host`, "https://github.com/acme", chromedp.ByQuery),
		chromedp.SendKeys(`#credential-token`, browserSecret, chromedp.ByQuery),
		clickButton(`main form.credential-form`, "Add credential"),
		credPoll(`(`+credNames+`) === 'work-token'`),
		chromedp.Evaluate(noSecret, &clean),
		chromedp.Evaluate(credNames, &names),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("the token is still in the page, an input or storage after it was added")
	}
	got, _ := h.fake.ListGitCredentials(context.Background())
	if list := got.Credentials; len(list) != 1 || list[0].Host != "github.com" || !list[0].IsDefault {
		t.Errorf("stored: %+v (the host normalised, the first token of a host its default)", list)
	}
	if err := chromedp.Run(other, credPoll(`(`+credNames+`) === 'work-token'`)); err != nil {
		t.Errorf("the second browser does not see it live: %v", err)
	}
	if got := errs.all(); len(got) > 0 {
		t.Errorf("console errors: %v", got)
	}
}

func TestAGeneratedKeyShowsItsPublicHalfAndIsDeletedAfterTheConfirm(t *testing.T) {
	h := newCredentialsHarness(t)
	ctx, errs := browser(t)
	var key, after string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/git-credentials?host=github.com&kind=ssh_key"),
		credPoll(credPageLive),
		credPoll(`document.querySelector('#credential-name').checkVisibility() && document.querySelector('#credential-kind-key').checked`),
		chromedp.SendKeys(`#credential-name`, "laptop-key", chromedp.ByQuery),
		clickButton(`main form.credential-form`, "Generate key"),
		credPoll(`!!document.querySelector('main .key-line code')?.textContent.startsWith('ssh-ed25519 ')`),
		chromedp.Evaluate(`document.querySelector('main .key-line code').textContent`, &key),
		// Delete from the row's menu, then the confirm (clicked through the DOM: top-layer popups stall chromedp's selectors).
		chromedp.Evaluate(`document.querySelector('main .credential-row .menu > button').click(); true`, nil),
		credPoll(`document.querySelector('main .credential-row [role="menu"]').matches(':popover-open') || document.querySelector('main .credential-row [role="menu"]').checkVisibility()`),
		chromedp.Evaluate(`document.querySelector('main .credential-row [data-action="delete"]').click(); true`, nil),
		credPoll(`document.querySelector('#credential-delete').checkVisibility() && document.querySelector('#credential-delete-text').textContent.includes('laptop-key')`),
		chromedp.Evaluate(`[...document.querySelectorAll('#credential-delete button')].find(b => b.textContent.trim() === 'Delete credential').click(); true`, nil),
		credPoll(`(`+credNames+`) === ''`),
		chromedp.Evaluate(credNames, &after),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := h.fake.ListGitCredentials(context.Background()); len(got.Credentials) != 0 {
		t.Errorf("still stored: %+v", got.Credentials)
	}
	if got := errs.all(); len(got) > 0 {
		t.Errorf("console errors: %v", got)
	}
}

func TestTestAndIdentityInTheBrowser(t *testing.T) {
	h := newCredentialsHarness(t)
	if _, err := h.fake.CreateGitCredential(context.Background(), ports.CreateGitCredential{Name: "work-token", Kind: domain.GitHTTPSToken, Host: "github.com", Token: browserSecret, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	ctx, errs := browser(t)
	var testLine string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/git-credentials"),
		credPoll(credPageLive),
		credPoll(`(`+credNames+`) === 'work-token'`),
		clickButton(`main .credential-row`, "Test"),
		credPoll(`!!document.querySelector('main .credential-row .banner[role="status"]')?.textContent.trim()`),
		chromedp.Evaluate(`document.querySelector('main .credential-row .banner[role="status"]').textContent.trim()`, &testLine),
		chromedp.SendKeys(`#identity-name`, "Ada Lovelace", chromedp.ByQuery),
		chromedp.SendKeys(`#identity-email`, "ada@example.com", chromedp.ByQuery),
		clickButton(`main section[aria-labelledby="identity-title"]`, "Save identity"),
		credPoll(`document.querySelector('#identity-notice').textContent.startsWith('Saved')`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if testLine == "" {
		t.Error("Test says what the server answered")
	}
	if id, _ := h.fake.GetGitIdentity(context.Background()); id == nil || id.Name != "Ada Lovelace" || id.Email != "ada@example.com" {
		t.Errorf("identity = %+v", id)
	}
	if got := errs.all(); len(got) > 0 {
		t.Errorf("console errors: %v", got)
	}
}

func TestRepositoriesArePinnedFromTheirRowInTheBrowser(t *testing.T) {
	h := newCredentialsHarness(t)
	bg := context.Background()
	if _, err := h.fake.CreateGitCredential(bg, ports.CreateGitCredential{Name: "work-token", Kind: domain.GitHTTPSToken, Host: "github.com", Token: browserSecret, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	spare, err := h.fake.CreateGitCredential(bg, ports.CreateGitCredential{Name: "spare-token", Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_spare0000"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, errs := browser(t)
	row := func(name string) string {
		return `[...document.querySelectorAll('main .row')].find(r => r.querySelector('[x-data="repositoryCredential"]') && r.textContent.includes('` + name + `'))`
	}
	line := func(name string) string {
		return `(` + row(name) + `)?.querySelector('[x-data="repositoryCredential"] > span')?.textContent.replace(/\s+/g, ' ').trim()`
	}
	var harness, infra string
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(h.srv.URL+"/projects/"+h.projectID+"/repositories"),
		credPoll(`!document.querySelector('main [data-ssr]')`),
		credPoll(`(`+line("harness")+` ?? '').includes('work-token')`),
		chromedp.Evaluate(line("infra"), &infra),
		// From the keyboard: the Listbox select opens on Enter on Automatic; Down to spare-token, Enter picks it.
		credPoll(`(`+row("harness")+`).querySelector('.dropdown .trigger')?.getAttribute('aria-labelledby') && document.getElementById((`+row("harness")+`).querySelector('.dropdown .trigger').getAttribute('aria-labelledby').split(' ')[0]).textContent === 'Credential'`),
		chromedp.Evaluate(`(`+row("harness")+`).querySelector('.dropdown .trigger').focus(); true`, nil),
		rawKey("Enter", 13),
		credPoll(`(`+row("harness")+`).querySelector('.dropdown .trigger').getAttribute('aria-expanded') === 'true'`),
		rawKey("ArrowDown", 40),
		rawKey("Enter", 13),
		credPoll(`(`+line("harness")+` ?? '').includes('spare-token · pinned')`),
		chromedp.Evaluate(line("harness"), &harness),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rc, _ := h.fake.GetRepositoryCredential(bg, h.repos["harness"]); rc.Binding.CredentialID == nil || *rc.Binding.CredentialID != spare.ID {
		t.Errorf("binding = %+v", rc.Binding)
	}
	if want := "No SSH key for github.com"; !strings.Contains(infra, want) {
		t.Errorf("infra says %q, want %q", infra, want)
	}
	if got := errs.all(); len(got) > 0 {
		t.Errorf("console errors: %v", got)
	}
}

func TestGitCredentialsPageFitsAPhone(t *testing.T) {
	h := newCredentialsHarness(t)
	if _, err := h.fake.CreateGitCredential(context.Background(), ports.CreateGitCredential{Name: "a-rather-long-credential-name-for-a-phone", Kind: domain.GitSSHKey, Host: "git.example-enterprise-host.com:2222", Generate: true}); err != nil {
		t.Fatal(err)
	}
	ctx, _ := browser(t)
	var overflow bool
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844, chromedp.EmulateMobile),
		chromedp.Navigate(h.srv.URL+"/git-credentials"),
		credPoll(credPageLive),
		chromedp.Evaluate(`[...document.querySelectorAll('main .credential-row button')].find(b => b.textContent.trim() === 'Public key').click(); true`, nil),
		credPoll(`!!document.querySelector('main .key-line')`),
		clickButton(`main header`, "Add credential"),
		credPoll(`document.querySelector('#credential-name').checkVisibility()`),
		chromedp.Evaluate(`document.documentElement.scrollWidth > document.documentElement.clientWidth`, &overflow),
	)
	if err != nil {
		t.Fatal(err)
	}
	if overflow {
		t.Error("the page scrolls sideways at 390px")
	}
}
