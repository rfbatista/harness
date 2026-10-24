package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// fakeSettingsAPI is the settings store behind the Models page: the page
// reads it, POST /api/update_settings writes it (or refuses).
type fakeSettingsAPI struct {
	mu     sync.Mutex
	values map[string]string
	writes []string
	refuse bool
}

func (f *fakeSettingsAPI) Setting(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.values[key]
}

func (f *fakeSettingsAPI) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.writes, "\n")
}

func (f *fakeSettingsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/api/update_settings" {
		http.NotFound(w, r)
		return
	}
	var in struct {
		Settings map[string]string `json:"settings"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(in.Settings)
	f.writes = append(f.writes, string(b))
	if f.refuse {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_INPUT", "error": "The model was not saved."})
		return
	}
	for k, v := range in.Settings {
		f.values[k] = v
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"settings": in.Settings})
}

func modelsBrowser(t *testing.T, store *fakeSettingsAPI, helper shell.Helper) (context.Context, string, *errorLog) {
	t.Helper()
	assets, err := NewAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !assets.Built() {
		t.Skip("browser test: web client not built; run `make web`")
	}
	ctx, errs := browser(t)
	pages := NewHandler(Deps{
		Projects:      fakeProjects{[]*domain.Project{{ID: "p1", Name: "coding_pool"}}},
		Tasks:         fakeTickets{},
		Sessions:      fakeSessions{},
		Agents:        fakeAgents{},
		Repositories:  fakeRepos{},
		Settings:      store,
		WritingHelper: func(context.Context) shell.Helper { return helper },
	}, assets, nil)
	mux := http.NewServeMux()
	mux.Handle("/api/", store)
	mux.Handle("/", pages)
	srv := newBrowserServer(mux)
	t.Cleanup(srv.Close)
	return ctx, srv.URL, errs
}

const assistantSelect = `[data-setting="models.assistants.writing_helper"] select`

func TestModelsPageWritingHelperRowInTheBrowser(t *testing.T) {
	store := &fakeSettingsAPI{values: map[string]string{}}
	ctx, url, errs := modelsBrowser(t, store, whAvailable)

	var heading, labels, current, announced, afterReload string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/models"),
		chromedp.WaitVisible(assistantSelect, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('#models-assistants-title').textContent`, &heading),
		chromedp.Evaluate(`[...document.querySelectorAll('`+assistantSelect+` option')].map((o) => o.value + '=' + o.textContent).join(',')`, &labels),
		chromedp.Evaluate(`document.querySelector('`+assistantSelect+`').value`, &current),
		whShot("models"),
		whPoll(`!!document.querySelector('[data-setting="models.assistants.writing_helper"]')._x_dataStack`),
		setField(assistantSelect, "sonnet", "change"),
		whPoll(`document.querySelector('[data-setting="models.assistants.writing_helper"] [role=status]').textContent === 'Model set to sonnet.'`),
		chromedp.Evaluate(`document.querySelector('[data-setting="models.assistants.writing_helper"] [role=status]').textContent`, &announced),
		chromedp.Navigate(url+"/models"),
		chromedp.WaitVisible(assistantSelect, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('`+assistantSelect+`').value`, &afterReload),
	)
	if err != nil {
		t.Fatalf("models: %v\nJS errors: %v", err, errs.all())
	}
	if heading != "Assistants" {
		t.Errorf("heading = %q", heading)
	}
	if labels != "haiku=haiku (default),sonnet=sonnet,opus=opus,fable=fable" || current != "haiku" {
		t.Errorf("choices %q, current %q", labels, current)
	}
	if announced != "Model set to sonnet." || afterReload != "sonnet" {
		t.Errorf("announced %q, after reload %q", announced, afterReload)
	}
	if got := store.log(); got != `{"models.assistants.writing_helper":"sonnet"}` {
		t.Errorf("saved %q", got)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestModelsPageWritingHelperSaveFailureInTheBrowser(t *testing.T) {
	store := &fakeSettingsAPI{values: map[string]string{}, refuse: true}
	ctx, url, errs := modelsBrowser(t, store, whAvailable)

	var banner, current string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/models"),
		chromedp.WaitVisible(assistantSelect, chromedp.ByQuery),
		whPoll(`!!document.querySelector('[data-setting="models.assistants.writing_helper"]')._x_dataStack`),
		setField(assistantSelect, "opus", "change"),
		whPoll(`!!document.querySelector('[data-setting="models.assistants.writing_helper"] [role=alert]')`),
		chromedp.Evaluate(`document.querySelector('[data-setting="models.assistants.writing_helper"] [role=alert]').textContent.replace(/\s+/g, ' ').trim()`, &banner),
		chromedp.Evaluate(`document.querySelector('`+assistantSelect+`').value`, &current),
		whShot("models-save-failed"),
		whClick(`[data-setting="models.assistants.writing_helper"]`, "Dismiss"),
		whPoll(`!document.querySelector('[data-setting="models.assistants.writing_helper"] [role=alert]')`),
	)
	if err != nil {
		t.Fatalf("failure: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "The model was not saved.") || !strings.Contains(banner, "INVALID_INPUT") || current != "haiku" {
		t.Errorf("banner %q, select back at %q", banner, current)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

func TestModelsPageSaysWhenTheHelperIsUnavailableInTheBrowser(t *testing.T) {
	store := &fakeSettingsAPI{values: map[string]string{}}
	ctx, url, errs := modelsBrowser(t, store, shell.Helper{Reason: "the harness's Claude is not signed in", FixURL: "/agent-isolation#credential"})

	var banner, href string
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(url+"/models"),
		chromedp.WaitVisible(assistantSelect, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-setting="models.assistants.writing_helper"] .banner[data-tone=attention]').textContent.replace(/\s+/g, ' ').trim()`, &banner),
		chromedp.Evaluate(`document.querySelector('[data-setting="models.assistants.writing_helper"] .banner a').getAttribute('href')`, &href),
		whShot("models-unavailable"),
	)
	if err != nil {
		t.Fatalf("unavailable: %v\nJS errors: %v", err, errs.all())
	}
	if !strings.Contains(banner, "Pens are disabled: the harness's Claude is not signed in") || href != "/agent-isolation#credential" {
		t.Errorf("banner %q, href %q", banner, href)
	}
	// The session tiers say nothing of it.
	var tierBanners int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('.banner[data-tone=attention]').length`, &tierBanners)); err != nil || tierBanners != 1 {
		t.Errorf("attention banners = %d (%v), want the helper's only", tierBanners, err)
	}
	if e := errs.all(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}
