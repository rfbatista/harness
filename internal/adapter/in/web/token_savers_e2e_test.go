//go:build !windows

package web_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/web"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/domain"
)

// The Token savers page, end to end: the capabilities context with its
// installations in SQLite, the HTTP API and the pages, a fake release server
// standing in for GitHub, and a person in headless Chrome. They reach the
// page from the top bar, install caveman and codegraph and update an rtk the
// harness pinned at an older version, see each at the pinned version (also
// after a reload), and remove codegraph. Files land under <data dir>/tools
// and caveman keeps only its SKILL.md. The old rtk stays, for sessions that
// may still run it, until the harness starts again.
func TestTokenSaversInstallUpdateAndRemoveInTheWebUI_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	rel := newFakeReleases()
	rel.serveRTK("v1.0.0", rtkArchive("rtk 1.0.0"), true)
	rel.serveRTK("v1.0.1", rtkArchive("rtk 1.0.1"), true)
	rel.serveCodegraph(codegraphArchive(), true)
	skill := cavemanSkill("Say less.")
	rel.serveCaveman(skill)
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)

	h := newTokenSaversHarness(t, srv.URL)
	// The harness that pinned rtk v1.0.0 installed it; this one pins v1.0.1.
	older := h.releases(digestOf(skill))
	rtkRelease(older).Version = "v1.0.0"
	h.installBefore(older, domain.TokenSaverRTK)
	if _, err := os.Stat(filepath.Join(h.tools, "rtk", "v1.0.0", "rtk")); err != nil {
		t.Fatalf("the older rtk is not installed: %v", err)
	}
	// codegraph's download waits until the page has shown its progress.
	release := rel.hold("/colbymchenry/codegraph/releases/download/v1.6.2/codegraph-darwin-arm64.tar.gz")
	page := h.serve(assets, h.releases(digestOf(skill)), darwinARM64)

	b := newBrowser(t)
	b.run("open the page from the top bar",
		chromedp.Navigate(page+"/"),
		waitUntil(`!!document.querySelector('.topbar a[href="/token-savers"]')`),
		chromedp.Evaluate(`document.querySelector('.topbar a[href="/token-savers"]').click()`, nil),
		atPath(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
		waitUntil(stateOf("rtk")+` === 'outdated' && `+rowText("rtk")+`.includes('v1.0.0') && `+rowText("rtk")+`.includes('v1.0.1')`),
		waitUntil(stateOf("caveman")+` === 'not_installed' && `+rowText("caveman")+`.includes('v4.0.1')`),
		waitUntil(stateOf("codegraph")+` === 'not_installed' && `+rowText("codegraph")+`.includes('v1.6.2')`),
	)
	b.run("install codegraph and see its progress",
		clickButton(row("codegraph"), "Install"),
		waitUntil(stateOf("codegraph")+` === 'installing' && `+rowText("codegraph")+`.includes('Downloading')`),
	)
	close(release)
	b.run("install caveman, update rtk",
		clickButton(row("caveman"), "Install"),
		clickButton(row("rtk"), "Update"),
		waitUntil(stateOf("codegraph")+` === 'installed' && `+rowText("codegraph")+`.includes('v1.6.2')`),
		waitUntil(stateOf("caveman")+` === 'installed' && `+rowText("caveman")+`.includes('v4.0.1')`),
		waitUntil(stateOf("rtk")+` === 'installed' && !`+rowText("rtk")+`.includes('v1.0.0')`),
	)
	b.run("the installations survive a reload",
		chromedp.Reload(),
		waitUntil(savers+`.length === 3`),
		waitUntil(`['rtk', 'caveman', 'codegraph'].every(s => `+row("")+`)`),
		waitUntil(stateOf("rtk")+` === 'installed' && `+stateOf("caveman")+` === 'installed' && `+stateOf("codegraph")+` === 'installed'`),
	)

	h.wantFile(filepath.Join("rtk", "v1.0.1", "rtk"), "#!/bin/sh\necho rtk 1.0.1\n", 0o100)
	// A running session may still use rtk v1.0.0: it stays until the
	// harness restarts.
	h.wantFile(filepath.Join("rtk", "v1.0.0", "rtk"), "#!/bin/sh\necho rtk 1.0.0\n", 0o100)
	h.wantFile(filepath.Join("codegraph", "v1.6.2", "codegraph-darwin-arm64", "bin", "codegraph"), "#!/bin/sh\necho codegraph\n", 0o100)
	h.wantFile(filepath.Join("caveman", "v4.0.1", "SKILL.md"), "Say less.", 0)
	h.wantGone(filepath.Join("caveman", "v4.0.1", "README.md"))
	h.wantGone(filepath.Join("caveman", "v4.0.1", "caveman"))
	installed, err := h.caps.InstalledTokenSavers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.TokenSaver]string{}
	for _, in := range installed {
		got[in.Saver] = in.Version + " " + in.Path
	}
	want := map[domain.TokenSaver]string{
		domain.TokenSaverRTK:       "v1.0.1 " + filepath.Join(h.tools, "rtk", "v1.0.1", "rtk"),
		domain.TokenSaverCaveman:   "v4.0.1 " + filepath.Join(h.tools, "caveman", "v4.0.1", "SKILL.md"),
		domain.TokenSaverCodegraph: "v1.6.2 " + filepath.Join(h.tools, "codegraph", "v1.6.2", "codegraph-darwin-arm64", "bin", "codegraph"),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("installed savers:\n%v\nwant:\n%v", got, want)
	}
	// No session survives a restart: starting again prunes the old version.
	h.service(h.releases(digestOf(skill)), darwinARM64)
	h.wantGone(filepath.Join("rtk", "v1.0.0"))
	h.wantFile(filepath.Join("rtk", "v1.0.1", "rtk"), "#!/bin/sh\necho rtk 1.0.1\n", 0o100)

	b.run("remove codegraph",
		clickButton(row("codegraph"), "Remove"),
		waitUntil(stateOf("codegraph")+` === 'not_installed'`),
		chromedp.Reload(),
		waitUntil(savers+`.length === 3 && `+stateOf("codegraph")+` === 'not_installed' && `+stateOf("rtk")+` === 'installed'`),
	)
	h.wantGone("codegraph")
	h.wantOnly("caveman", "rtk")
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// Downloads that cannot be verified are refused with a readable error and
// leave nothing under <data dir>/tools: rtk's archive does not match its
// line in checksums.txt, caveman's does not match the digest the harness
// pins, and codegraph's asset is missing from its release. Then a caveman
// whose digest does match installs.
func TestTokenSaverDownloadsThatFailVerificationLeaveNothingInTheWebUI_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	rel := newFakeReleases()
	rel.serveRTK("v1.0.1", rtkArchive("rtk 1.0.1"), false)
	rel.serveCodegraph(nil, true)
	skill := cavemanSkill("Say less.")
	rel.serveCaveman(skill)
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)

	h := newTokenSaversHarness(t, srv.URL)
	wrong := strings.Repeat("0", 64)
	page := h.serve(assets, h.releases(wrong), darwinARM64)

	b := newBrowser(t)
	b.run("open the page",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
	)
	b.run("rtk's checksum does not match",
		clickButton(row("rtk"), "Install"),
		waitUntil(stateOf("rtk")+` === 'failed'`),
		waitUntil(rowText("rtk")+`.includes("rtk-oss-aarch64-apple-darwin.tar.gz does not match its checksum in checksums.txt")`),
		waitUntil(rowText("rtk")+`.includes('TOKEN_SAVER_CHECKSUM_MISMATCH')`),
	)
	b.run("caveman's pinned digest does not match",
		clickButton(row("caveman"), "Install"),
		waitUntil(stateOf("caveman")+` === 'failed'`),
		waitUntil(rowText("caveman")+`.includes("caveman.skill does not match the sha256 the harness pins")`),
	)
	b.run("codegraph's asset is missing",
		clickButton(row("codegraph"), "Install"),
		waitUntil(stateOf("codegraph")+` === 'failed'`),
		waitUntil(rowText("codegraph")+`.includes("codegraph v1.6.2 has no asset codegraph-darwin-arm64.tar.gz")`),
		waitUntil(rowText("codegraph")+`.includes('TOKEN_SAVER_ASSET_MISSING')`),
	)
	h.wantOnly()

	// The same page, now pinning caveman's real digest.
	page = h.serve(assets, h.releases(digestOf(skill)), darwinARM64)
	b.run("caveman's pinned digest matches",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3 && `+stateOf("rtk")+` === 'not_installed'`),
		clickButton(row("caveman"), "Install"),
		waitUntil(stateOf("caveman")+` === 'installed'`),
	)
	h.wantFile(filepath.Join("caveman", "v4.0.1", "SKILL.md"), "Say less.", 0)
	h.wantOnly("caveman")
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// On a platform the harness has no releases for, the page says which ones it
// supports and Install is refused without downloading anything.
func TestTokenSaversRefuseAnUnsupportedPlatformInTheWebUI_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	rel := newFakeReleases()
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)

	h := newTokenSaversHarness(t, srv.URL)
	page := h.serve(assets, h.releases(strings.Repeat("0", 64)), domain.Platform{OS: "windows", Arch: "amd64"})

	const supported = "darwin/amd64, darwin/arm64, linux/amd64 and linux/arm64"
	b := newBrowser(t)
	b.run("the page names the supported platforms",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
		waitUntil(`document.querySelector('main').innerText.includes('windows/amd64') && document.querySelector('main').innerText.includes(`+quote(supported)+`)`),
	)
	b.run("Install is refused",
		clickButton(row("rtk"), "Install"),
		waitUntil(`[...document.querySelectorAll('main [role=alert]')].some(a => a.innerText.includes('TOKEN_SAVER_UNSUPPORTED_PLATFORM') && a.innerText.includes(`+quote(supported)+`))`),
		waitUntil(stateOf("rtk")+` === 'not_installed'`),
	)
	if got := rel.requests(); len(got) > 0 {
		t.Errorf("requests to the release server: %v", got)
	}
	h.wantOnly()
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

// The caveman intensity is full until the person sets it on the Token
// savers page; ultra then stays across a reload and is what sessions get.
func TestCavemanIntensityIsSetOnTheTokenSaversPage_EndToEnd(t *testing.T) {
	assets := builtAssets(t)
	h := newTokenSaversHarness(t, "http://127.0.0.1:1")
	page := h.serve(assets, h.releases(strings.Repeat("0", 64)), darwinARM64)

	const intensity = `(document.querySelector('#caveman-intensity')?.value ?? '')`
	b := newBrowser(t)
	b.run("the intensity is full by default",
		chromedp.Navigate(page+"/token-savers"),
		waitUntil(savers+`.length === 3`),
		waitUntil(intensity+` === 'full'`),
	)
	b.run("set it to ultra",
		chromedp.Evaluate(`(() => { const s = document.querySelector('#caveman-intensity');
			s.value = 'ultra'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`, nil),
		waitUntil(`[...document.querySelectorAll('main [aria-live]')].some(e => e.textContent.includes('Caveman intensity set to ultra'))`),
	)
	b.run("ultra stays across a reload",
		chromedp.Reload(),
		waitUntil(savers+`.length === 3`),
		waitUntil(intensity+` === 'ultra'`),
	)
	if got := h.caps.CavemanIntensity(t.Context()); got != domain.CavemanUltra {
		t.Errorf("sessions get caveman at %q, want ultra", got)
	}
	if e := b.jsErrors(); len(e) > 0 {
		t.Errorf("JavaScript errors:\n%s", strings.Join(e, "\n"))
	}
}

var darwinARM64 = domain.Platform{OS: "darwin", Arch: "arm64"}

const savers = `document.querySelectorAll('main [data-saver]:not([data-ssr] *)')`

// row is the live row of saver (s, the JS variable, when saver is "").
func row(saver string) string {
	sel := quote(`main [data-saver="` + saver + `"]`)
	if saver == "" {
		sel = "`main [data-saver=\"${s}\"]`"
	}
	return `[...document.querySelectorAll(` + sel + `)].find(r => !r.closest('[data-ssr]'))`
}

func stateOf(saver string) string { return row(saver) + `?.dataset.state` }

func rowText(saver string) string { return `(` + row(saver) + `?.innerText ?? '')` }

func builtAssets(t *testing.T) *web.Assets {
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
	return assets
}

// tokenSaversHarness is the server side: one database and one data
// directory, served by as many capabilities services as a test asks for (a
// harness that pinned other versions, then this one).
type tokenSaversHarness struct {
	t     *testing.T
	db    string
	tools string
	base  string
	caps  *capabilities.Service
}

func newTokenSaversHarness(t *testing.T, releaseBase string) *tokenSaversHarness {
	dir := t.TempDir()
	return &tokenSaversHarness{t: t, db: filepath.Join(dir, "data.db"), tools: filepath.Join(dir, "tools"), base: releaseBase}
}

// releases is the manifest the harness ships, with caveman's digest pinned
// to cavemanDigest: no stub archive can match the real one.
func (h *tokenSaversHarness) releases(cavemanDigest string) []capabilities.TokenSaverRelease {
	list := capabilities.PinnedTokenSaverReleases()
	for i := range list {
		if list[i].Saver == domain.TokenSaverCaveman {
			list[i].SHA256 = cavemanDigest
		}
	}
	return list
}

func rtkRelease(list []capabilities.TokenSaverRelease) *capabilities.TokenSaverRelease {
	for i := range list {
		if list[i].Saver == domain.TokenSaverRTK {
			return &list[i]
		}
	}
	panic("no rtk in the manifest")
}

func (h *tokenSaversHarness) service(list []capabilities.TokenSaverRelease, platform domain.Platform) catalog.Catalog {
	h.t.Helper()
	db, err := sqlite.Open(h.db)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = sqlite.Close(db) })
	cat := catalog.New(catalog.SQLiteDeps(db))
	cat.Capabilities.UseTokenSavers(capabilities.TokenSaverDeps{
		Installations:  sqlite.NewTokenSaverInstallationRepository(db),
		Downloads:      releases.NewClient(),
		Dir:            h.tools,
		ReleaseBaseURL: h.base,
		Releases:       list,
		Platform:       platform,
	})
	h.caps = cat.Capabilities
	return cat
}

// installBefore installs savers the way a harness pinning list did.
func (h *tokenSaversHarness) installBefore(list []capabilities.TokenSaverRelease, savers ...domain.TokenSaver) {
	h.t.Helper()
	caps := h.service(list, darwinARM64).Capabilities
	for _, s := range savers {
		if _, err := caps.InstallTokenSaver(h.t.Context(), s); err != nil {
			h.t.Fatal(err)
		}
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		list, err := caps.ListTokenSavers(h.t.Context())
		if err != nil {
			h.t.Fatal(err)
		}
		done := true
		for _, st := range list.Savers {
			if slices.Contains(savers, st.Saver) && st.State != domain.TokenSaverInstalled {
				done = false
				if st.Error != nil {
					h.t.Fatalf("%s: %s", st.Saver, st.Error.Message)
				}
			}
		}
		if done {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("still installing: %+v", list)
		}
	}
}

// serve starts the pages and the API over a service pinning list on platform.
func (h *tokenSaversHarness) serve(assets *web.Assets, list []capabilities.TokenSaverRelease, platform domain.Platform) string {
	cat := h.service(list, platform)
	caps := cat.Capabilities
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{TokenSaverInstalls: caps, Settings: cat.Settings})))
	mux.Handle("/", web.NewHandler(web.Deps{Projects: cat.Projects, TokenSaverInstalls: caps}, assets, nil))
	srv := httptest.NewServer(mux)
	h.t.Cleanup(srv.Close)
	return srv.URL
}

// wantFile checks a file under the tools directory: its content and, when
// mode is set, those permission bits.
func (h *tokenSaversHarness) wantFile(rel, content string, mode os.FileMode) {
	h.t.Helper()
	path := filepath.Join(h.tools, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		h.t.Errorf("%s: %v", rel, err)
		return
	}
	if string(b) != content {
		h.t.Errorf("%s = %q, want %q", rel, b, content)
	}
	if info, err := os.Stat(path); err == nil && info.Mode()&mode != mode {
		h.t.Errorf("%s mode = %v, want %v set", rel, info.Mode(), mode)
	}
}

func (h *tokenSaversHarness) wantGone(rel string) {
	h.t.Helper()
	if _, err := os.Stat(filepath.Join(h.tools, rel)); !os.IsNotExist(err) {
		h.t.Errorf("%s is still there (%v)", rel, err)
	}
}

// wantOnly checks that the tools directory holds exactly names: no other
// saver and nothing a failed download left.
func (h *tokenSaversHarness) wantOnly(names ...string) {
	h.t.Helper()
	entries, err := os.ReadDir(h.tools)
	if err != nil && !os.IsNotExist(err) {
		h.t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(names)
	if strings.Join(got, ",") != strings.Join(names, ",") {
		h.t.Errorf("tools directory holds %v, want %v", got, names)
	}
}

// fakeReleases serves release assets and checksum files at GitHub's paths,
// and can hold one back until the test lets it go.
type fakeReleases struct {
	mu    sync.Mutex
	files map[string][]byte
	holds map[string]chan struct{}
	seen  []string
}

func newFakeReleases() *fakeReleases {
	return &fakeReleases{files: map[string][]byte{}, holds: map[string]chan struct{}{}}
}

func (f *fakeReleases) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.URL.Path)
	body, ok := f.files[r.URL.Path]
	hold := f.holds[r.URL.Path]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if hold != nil {
		// Half now, so the page has progress to show; the rest once released.
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
		body = body[len(body)/2:]
	}
	_, _ = w.Write(body)
}

func (f *fakeReleases) hold(path string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan struct{})
	f.holds[path] = ch
	return ch
}

func (f *fakeReleases) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func (f *fakeReleases) put(path string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = body
}

// serveRTK publishes rtk's darwin/arm64 archive with a checksums.txt that
// matches it, or names another digest.
func (f *fakeReleases) serveRTK(version string, archive []byte, matching bool) {
	const asset = "rtk-oss-aarch64-apple-darwin.tar.gz"
	digest := digestOf(archive)
	if !matching {
		digest = digestOf([]byte("something else"))
	}
	dir := "/rtk-ai/rtk/releases/download/" + version + "/"
	f.put(dir+asset, archive)
	f.put(dir+"checksums.txt", []byte(digestOf([]byte("other"))+"  rtk-oss-x86_64-apple-darwin.tar.gz\n"+digest+"  "+asset+"\n"))
}

// serveCodegraph publishes codegraph's SHA256SUMS and, unless archive is
// nil, its darwin/arm64 bundle.
func (f *fakeReleases) serveCodegraph(archive []byte, withSums bool) {
	const asset = "codegraph-darwin-arm64.tar.gz"
	dir := "/colbymchenry/codegraph/releases/download/v1.6.2/"
	sum := digestOf([]byte("missing"))
	if archive != nil {
		f.put(dir+asset, archive)
		sum = digestOf(archive)
	}
	if withSums {
		f.put(dir+"SHA256SUMS", []byte(sum+"  "+asset+"\n"))
	}
}

func (f *fakeReleases) serveCaveman(skill []byte) {
	f.put("/JuliusBrussee/caveman/releases/download/v4.0.1/caveman.skill", skill)
}

func rtkArchive(says string) []byte {
	return tarGz(map[string]string{"rtk": "#!/bin/sh\necho " + says + "\n", "LICENSE": "Apache-2.0"}, "rtk")
}

func codegraphArchive() []byte {
	const root = "codegraph-darwin-arm64/"
	return tarGz(map[string]string{
		root + "node":                 "#!/bin/sh\n",
		root + "bin/codegraph":        "#!/bin/sh\necho codegraph\n",
		root + "lib/dist/bin/main.js": "console.log('codegraph')",
	}, root+"node", root+"bin/codegraph")
}

func cavemanSkill(rules string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{{"caveman/SKILL.md", rules}, {"caveman/README.md", "# caveman"}} {
		w, err := zw.Create(f.name)
		if err != nil {
			panic(err)
		}
		_, _ = w.Write([]byte(f.body))
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// tarGz archives files (path → content), executables mode 0755, with the
// directories above them as entries of their own, as real releases have.
func tarGz(files map[string]string, executables ...string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	dirs := map[string]bool{}
	for _, name := range names {
		for d := filepath.Dir(name); d != "."; d = filepath.Dir(d) {
			if !dirs[d] {
				dirs[d] = true
				_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d + "/", Mode: 0o755})
			}
		}
		mode := int64(0o644)
		if slices.Contains(executables, name) {
			mode = 0o755
		}
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, Size: int64(len(files[name]))})
		_, _ = tw.Write([]byte(files[name]))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
