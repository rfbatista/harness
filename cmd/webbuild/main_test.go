package main

import (
	"os"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/rfbatista/gohydrate"
)

// web/test/all.js must list every *.test.js, or the browser suite silently
// skips a file. Regenerate with `go run ./cmd/webbuild`.
func TestTestListIsCurrent(t *testing.T) {
	want, err := TestList("../..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../" + testsList)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; run `go run ./cmd/webbuild`.\nwant:\n%s", testsList, want)
	}
	if !strings.Contains(want, `import "../src/modules/sessions/domain/session.test.js";`) {
		t.Fatalf("expected the sessions domain tests in the list:\n%s", want)
	}
}

func TestOptionsAliasAlpineToTheVendoredBuild(t *testing.T) {
	opts := options("/repo", false)
	if opts.Alias["alpinejs"] != alpineAlias {
		t.Fatalf("alias = %q", opts.Alias["alpinejs"])
	}
	if !opts.MinifyWhitespace || options("/repo", true).MinifyWhitespace {
		t.Fatal("production minifies; dev does not")
	}
	for name, path := range vendors {
		if _, err := os.Stat("../../" + strings.TrimPrefix(path, "./")); err != nil {
			t.Fatalf("vendored %s missing: %v", name, err)
		}
	}
}

// document.css is the stylesheet an HTML task document may link; it is
// built beside the app bundles, under its own name.
func TestDocumentStylesheetIsBuilt(t *testing.T) {
	opts := documentOptions("/repo", false)
	if len(opts.EntryPoints) != 1 || opts.EntryPoints[0] != "web/src/document.css" || opts.EntryNames != "document" || opts.Outdir != outDir {
		t.Fatalf("document build options = %+v", opts)
	}
	if _, err := os.Stat("../../web/src/document.css"); err != nil {
		t.Fatalf("web/src/document.css missing: %v", err)
	}
}

// The code worker is a second bundle, a classic script built beside app.js.
func TestCodeWorkerIsBuiltBesideTheApp(t *testing.T) {
	opts := workerOptions("/repo", false)
	if len(opts.EntryPoints) != 1 || opts.EntryPoints[0] != "web/src/code-worker.js" || opts.EntryNames != "code-worker" || opts.Outdir != outDir {
		t.Fatalf("worker build options = %+v", opts)
	}
	if opts.Format != api.FormatIIFE {
		t.Fatalf("a worker started with new Worker(url) is a classic script, got format %v", opts.Format)
	}
	if _, err := os.Stat("../../web/src/code-worker.js"); err != nil {
		t.Fatalf("worker entry missing: %v", err)
	}
}

// The React client builds through gohydrate: the server-rendered islands (the top
// bar, the rail and the header's Tasks button) and the browser's composition
// root, written beside the other bundles.
func TestIslandsAreBuiltThroughGohydrate(t *testing.T) {
	opts := islandOptions("/repo", false)
	want := map[string]string{"top-bar": "top-bar.server.tsx", "rail": "rail.server.tsx", "tasks-toggle": "tasks-toggle.server.tsx"}
	if len(opts.Islands) != len(want) {
		t.Fatalf("islands = %+v", opts.Islands)
	}
	for _, isl := range opts.Islands {
		if file, ok := want[isl.Name]; !ok || isl.ServerEntry != "/repo/web/app/src/islands/"+file {
			t.Fatalf("island %q: server entry %q", isl.Name, isl.ServerEntry)
		}
	}
	if opts.ClientEntry != "/repo/web/app/src/main.client.tsx" || opts.ResolveDir != "/repo/web/app" || opts.OutDir != "/repo/"+outDir {
		t.Fatalf("island build options = %+v", opts)
	}
	if !opts.Minify || islandOptions("/repo", true).Minify {
		t.Fatal("production minifies; dev does not")
	}
	for _, p := range []string{opts.Islands[0].ServerEntry, opts.ClientEntry} {
		if _, err := os.Stat("../.." + strings.TrimPrefix(p, "/repo")); err != nil {
			t.Fatalf("island entry missing: %v", err)
		}
	}
}

// Every rebuild of the client bundle changes its hash; the old files must not
// stay embedded in the binary.
func TestStaleIslandBundlesAreRemovedAfterABuild(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"islands-OLD1.js", "islands-OLD1.js.map", "islands-OLD1.css", "islands-NEW1.js", "islands-NEW1.js.map", "islands-NEW1.css", "islands.server.js", "manifest.json", "app.js"} {
		if err := os.WriteFile(dir+"/"+name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := gohydrate.Manifest{Client: gohydrate.ClientBundle{JS: "islands-NEW1.js", CSS: "islands-NEW1.css"}}
	if err := pruneStaleIslands(dir, m); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"islands-OLD1.js": false, "islands-OLD1.js.map": false, "islands-OLD1.css": false,
		"islands-NEW1.js": true, "islands-NEW1.js.map": true, "islands-NEW1.css": true,
		"islands.server.js": true, "manifest.json": true, "app.js": true,
	} {
		_, err := os.Stat(dir + "/" + name)
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
}
